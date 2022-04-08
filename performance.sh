# !/bin/bash
for i in file{1..1000};do
    curl -H HOST "localhost:3000/articles" -X "POST" -d '{"Title":"Sugar","Body":"MyJson body is pretty form-at-ed","Tags":["health","eco","abc"]}'
done

curl -H HOST "localhost:3000/tags/health/20220408"
