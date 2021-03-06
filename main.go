package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//Domain object model for receiving Post request data
type DtoPostRequestArticle struct {
	Title string
	Body  string
	Tags  []string
}

//Data transfer object(DTO) model for responding to request for articles
type DtoRespArticle struct {
	Id    int
	Title string
	Date  string
	Body  string
	Tags  []string
}

//Data transfer object(DTO) model for responding to request for articles
type DomainDataObject struct {
	Id    int
	Title string
	Date  string
	Body  string
	Tags  map[string]int
}

//Data transfer object(DTO) for responding to request for tag specific queries
type DtoResponseTagArticles struct {
	Tag          string   // Request tag
	Count        int      // Total articles found with tag
	Articles     []int    // List of ids for the last 10 articles entered for that day
	Related_Tags []string // Unique list of tags that are on the articles that the current tag is on for the same day
}

//Wrapper for the faster search algorithm support
//Two maps formed by [date]:[]id
//Two maps formed by [tag]:[]id
//Then a common intersection of the map will be the result
//
type searchOptimizer struct {
	//
	DateIdsMap map[string][]int
	TagIdsMap  map[string][]int
}

//Wrapper for the store
type articleHandlers struct {
	m     sync.Mutex
	ch    chan DtoPostRequestArticle
	store []DomainDataObject
	//enahnce search results
	so searchOptimizer
}

//Request parser / router
func (h *articleHandlers) articles(resp http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		h.get(resp, r)
		return
	case "POST":
		h.enqueuPostRequests(resp, r)
		return
	default:
		resp.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

//Request parser / router
func (h *articleHandlers) tags(resp http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		h.getTagsFaster(resp, r)
		return
	case "POST":
	default:
		resp.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

//
//Intersection of two slices
//
func Intersection(a, b []int) (c []int) {
	m := make(map[int]bool)

	for _, item := range a {
		m[item] = true
	}

	for _, item := range b {
		if _, ok := m[item]; ok {
			c = append(c, item)
		}
	}
	return
}

//
//GET - handler to process below curl requests
//curl http://localhost:3000/tags/health/20220407
//
func (h *articleHandlers) getTagsFaster(resp http.ResponseWriter, r *http.Request) {
	h.m.Lock()
	defer h.m.Unlock()
	ss := strings.Split(r.URL.String(), "/")
	//Only allow valid request curl http://localhost:3000/tags/health/20220407
	if len(ss) != 4 {
		resp.WriteHeader(http.StatusBadRequest)
		resp.Write([]byte("Bad request tag url."))
		return
	}

	dateQ := ss[3]
	idsByDate := h.so.DateIdsMap[dateQ]

	tagQ := ss[2]
	idsByTag := h.so.TagIdsMap[tagQ]

	finalIds := Intersection(idsByDate, idsByTag)
	var tagStore DtoResponseTagArticles
	tagStore.Tag = tagQ
	tagStore.Count = len(finalIds)
	tagStore.Articles = sort.IntSlice(finalIds)

	tagMap := make(map[string]int)

	for id := range finalIds {
		for k, _ := range h.store[id].Tags {
			tagMap[strings.ToLower(k)] = 0
		}
	}

	//map keys tags are flattened to slice for json dto
	for k, _ := range tagMap {
		tagStore.Related_Tags = append(tagStore.Related_Tags, k)
	}

	jsonBody, err := json.Marshal(tagStore)
	if err != nil {
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(err.Error()))
		return
	}
	resp.Header().Add("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
	resp.Write(jsonBody)
}

//
//GET - handler to process below curl requests
//curl http://localhost:3000/articles/0
//
func (h *articleHandlers) process_id_request(resp http.ResponseWriter, r *http.Request) {

	h.m.Lock()
	defer h.m.Unlock()
	ss := strings.Split(r.URL.String(), "/")
	number, errConv := strconv.ParseUint(ss[2], 10, 32)
	if errConv != nil {
		resp.WriteHeader(http.StatusBadRequest)
		resp.Write([]byte("Bad request url."))
		return
	}
	finalIntNum := int(number) //Convert uint64 To int
	if finalIntNum > len(h.store)-1 {
		resp.WriteHeader(http.StatusBadRequest)
		resp.Write([]byte("Bad request url - index not valid."))
		return
	}
	jsonBody, err := json.Marshal(h.store[finalIntNum])
	if err != nil {
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(err.Error()))
		return
	}

	resp.Header().Add("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
	resp.Write(jsonBody)
}

//
//GET - handler to process below curl requests
//curl http://localhost:3000/
//curl http://localhost:3000/articles
//curl http://localhost:3000/articles/0
//
func (h *articleHandlers) get(resp http.ResponseWriter, r *http.Request) {

	ss := strings.Split(r.URL.String(), "/")
	if len(ss) > 3 {
		resp.WriteHeader(http.StatusBadRequest)
		resp.Write([]byte("Bad request url."))
		return
	}
	if len(ss) == 3 {
		//Here we have requested to process id based query
		h.process_id_request(resp, r)
		return
	}

	h.m.Lock()
	defer h.m.Unlock()

	respStore := h.CreateRespStoreFromDomainStore()

	jsonBody, err := json.Marshal(respStore)
	if err != nil {
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(err.Error()))
		return
	}
	resp.Header().Add("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
	resp.Write(jsonBody)
}

//
//This function expects to queue multiple clients post request to avoid
//delay in writer
//
func (h *articleHandlers) enqueuPostRequests(resp http.ResponseWriter, r *http.Request) {
	//Send it to channel
	h.ch <- h.getDtoFromRequest(resp, r)
	//Return successful respone immediately
	resp.Header().Add("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
}

//
//Below function will process all requests buffered in blocking channel
//This is faster CQRS pattern to remove the write blocking
//Idea to remove locks but we still need it
//
func processPostQueue(h *articleHandlers) {

	for {
		dtoReq, open := <-h.ch
		if open == false {
			return
		}
		domain := h.ConvertReqDtoToDomain(dtoReq)
		h.m.Lock()
		h.store = append(h.store, domain)
		cleandate := strings.ReplaceAll(domain.Date, "-", "")
		h.so.DateIdsMap[cleandate] = append(h.so.DateIdsMap[cleandate], domain.Id)
		for k, _ := range domain.Tags {
			h.so.TagIdsMap[strings.ToLower(k)] = append(h.so.TagIdsMap[k], domain.Id)
		}

		h.m.Unlock()
	}
}

//
//Post handler
//respective post body is attached with postman query
//POST Domain object carries only Title,body and tags
//Example - of curl post request
//curl -H HOST "localhost:3000/articles" -X "POST" -d '{"Title":"Sugar","Body":"MyJson body is pretty form-at-ed","Tags":["health","fitness","science"]}'
//
func (h *articleHandlers) getDtoFromRequest(resp http.ResponseWriter, r *http.Request) DtoPostRequestArticle {

	var dtoReq DtoPostRequestArticle

	jsonBodyBytes, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		return dtoReq
	}

	err = json.Unmarshal(jsonBodyBytes, &dtoReq)
	if err != nil {
		return dtoReq
	}

	return dtoReq

}

//
//Request DTO is converted to domain dto before saving in repository
//Adapter pattern to transform data from one form to other
//
func (h *articleHandlers) ConvertReqDtoToDomain(dtoReq DtoPostRequestArticle) DomainDataObject {
	var domain DomainDataObject
	domain.Id = len(h.store)
	domain.Title = dtoReq.Title
	domain.Date = time.Now().Format("2006-01-02")
	domain.Body = dtoReq.Body
	domain.Tags = make(map[string]int)
	i := 0
	for ; i < len(dtoReq.Tags); i++ {
		domain.Tags[strings.ToLower(dtoReq.Tags[i])] = i
	}
	return domain
}

//
//Domain data is converted to DTO before saving in repository
//Adapter pattern to transform data from one form to other
//
func (h *articleHandlers) CreateRespStoreFromDomainStore() []DtoRespArticle {

	var s []DtoRespArticle
	for i := 0; i < len(h.store); i++ {
		domain := h.store[i]
		var dto DtoRespArticle
		dto.Id = domain.Id
		dto.Title = domain.Title
		dto.Date = domain.Date
		dto.Body = domain.Body
		dto.Tags = make([]string, len(h.store[i].Tags))
		for tag, _ := range h.store[i].Tags {
			dto.Tags = append(dto.Tags, tag)
		}
		s = append(s, dto)
	}
	return s
}

//Get default store data
func newArticleHandlers(postChannel chan DtoPostRequestArticle) *articleHandlers {
	return &articleHandlers{
		ch: postChannel,
		so: searchOptimizer{
			DateIdsMap: map[string][]int{
				"": []int{},
			},
			TagIdsMap: map[string][]int{
				"": []int{},
			},
		},
		//:make(chan DtoPostRequestArticle, 100), //Buffer 100 clients,
		store: []DomainDataObject{
			DomainDataObject{
				Id:    0,
				Title: "latest science shows that potato chips are better for you than sugar",
				Date:  time.Now().Format("2006-01-02"),
				Body:  "some text, potentially containing simple markup about how potato chips are great",
				Tags: map[string]int{
					"health":  0,
					"fitness": 1,
					"science": 2,
				},
			},
		},
	}
}

func main() {

	default_port := "3000"

	//Scalable architecture - to cater 100 clients request simultaneously
	max_clients := 100
	ch := make(chan DtoPostRequestArticle, max_clients)

	//Initializing default store
	articleHandlers := newArticleHandlers(ch)

	//Router handler registration
	http.HandleFunc("/", articleHandlers.articles)
	http.HandleFunc("/tags/", articleHandlers.tags)

	//Launch channel processing worker
	go processPostQueue(articleHandlers)

	//Launch server
	fmt.Println("server running at  :" + default_port)
	if err := http.ListenAndServe("localhost:"+default_port, nil); err != nil {
		log.Fatal(err)
	}
}
