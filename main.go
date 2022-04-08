package main
import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
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
//Wrapper for the store
type articleHandlers struct {
	m     sync.Mutex
	store []DomainDataObject
}
//Request parser / router
func (h *articleHandlers) articles(resp http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		h.get(resp, r)
		return
	case "POST":
		h.post(resp, r)
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
		h.getTags(resp, r)
		return
	case "POST":
	default:
		resp.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

//
//GET - handler to process below curl requests
//curl http://localhost:3000/tags/health/20220407
//
func (h *articleHandlers) getTags(resp http.ResponseWriter, r *http.Request) {
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
	tagQ := ss[2]
	var tagStore DtoResponseTagArticles
	tagMap := make(map[string]int)
	count := 0
	var ids []int
	//
	//Below code will search date and then iterate in tags to find query tag and date
	//combination
	//However GraphQL is expected to retrieve data faster and neat way
	//
	for i := 0; i < len(h.store); i++ {
		//clean search date string
		res := strings.ReplaceAll(h.store[i].Date, "-", "")
		if res == dateQ { // date check
			_,pres := h.store[i].Tags[tagQ] 
			if pres == true {
				//if we are here we have found a valid date and tag entry 
				//Now create the return response dto object
				ids = append(ids, h.store[i].Id )
				for k,_ := range h.store[i].Tags{
					tagMap[k] = 0
				}
				count++
			}
		}
	}

	//create tag dto
	tagStore.Count = count
	tagStore.Tag = tagQ
	tagStore.Articles = ids
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
	
	fmt.Println(respStore)

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
//write to block
// 
func (h *articleHandlers) enqueuPostRequests(resp http.ResponseWriter, r *http.Request) {

}

//
//Post handler
//respective post body is attached with postman query
//POST Domain object carries only Title,body and tags
//Example - of curl post request
//curl -H HOST "localhost:3000/articles" -X "POST" -d '{"Title":"Sugar","Body":"MyJson body is pretty form-at-ed","Tags":["health","fitness","science"]}'
//
func (h *articleHandlers) post(resp http.ResponseWriter, r *http.Request) {
	h.m.Lock()
	defer h.m.Unlock()
	jsonBodyBytes, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		resp.WriteHeader(http.StatusInternalServerError)
		resp.Write([]byte(err.Error()))
		return
	}
	var dtoReq DtoPostRequestArticle
	err = json.Unmarshal(jsonBodyBytes, &dtoReq)
	if err != nil {
		resp.WriteHeader(http.StatusBadRequest)
		resp.Write([]byte(err.Error()))
		return
	}

	//Adapter to translate request
	domain := h.ConvertReqDtoToDomain(dtoReq)
	h.store = append(h.store, domain)
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
	for ;i<len(dtoReq.Tags); i++{
		domain.Tags[dtoReq.Tags[i]] = i
	}
	return domain
}

//
//Domain data is converted to DTO before saving in repository
//Adapter pattern to transform data from one form to other
//
func (h *articleHandlers) CreateRespStoreFromDomainStore() []DtoRespArticle {
	
	var s []DtoRespArticle
	for i:=0; i< len(h.store); i++  {
		domain := h.store[i]
		var dto DtoRespArticle
		dto.Id = domain.Id
		dto.Title = domain.Title
		dto.Date = domain.Date
		dto.Body = domain.Body
		dto.Tags = make([]string, len(h.store[i].Tags))
		for tag,_ := range h.store[i].Tags {
			dto.Tags = append(dto.Tags, tag)
		}
		s = append(s, dto)
	} 
	return s
}

//Get default store data
func newArticleHandlers() *articleHandlers {
	return &articleHandlers{
		store: []DomainDataObject{
			DomainDataObject{
				Id:    0,
				Title: "latest science shows that potato chips are better for you than sugar",
				Date:  time.Now().Format("2006-01-02"),
				Body:  "some text, potentially containing simple markup about how potato chips are great",
				Tags: map[string]int{
					"health":0,
					"fitness":1,
					"science":2,
				},
			},
		},
	}
}
func main() {
	default_port := "3000"
	//Initializing default store
	articleHandlers := newArticleHandlers()
	//Router handler registration
	http.HandleFunc("/", articleHandlers.articles)
	http.HandleFunc("/tags/", articleHandlers.tags)
	fmt.Println("server running at  :" + default_port)
	if err := http.ListenAndServe("localhost:"+default_port, nil); err != nil {
		log.Fatal(err)
	}
}
