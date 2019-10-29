package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/labstack/echo"
)

func main() {
	t := os.Getenv("TYPE")

	switch t {
	case "MAP":
		mapper()
	case "MASTER":
		master()
	case "REDUCE":
		reducer()
	}
}

type obj struct {
	Positive float32
	Negative float32
}
type resultBody struct {
	Status      string
	Predictions []obj
}
type requestBody struct {
	Text []string `json:"text"`
}

func qHTTPRequests(x string) (pos float32) {
	url := "http://localhost:5000/model/predict"

	//preparing the object
	reqobj := requestBody{}
	reqobj.Text = append(reqobj.Text, x)

	//json conversion
	data, _ := json.Marshal(reqobj)

	//convert to byte[]
	payload := strings.NewReader(string(data))
	req, _ := http.NewRequest("POST", url, payload)
	req.Header.Add("content-type", "application/json")

	res, err := http.DefaultClient.Do(req)

	if err != nil {
		fmt.Println(err)
	}
	defer res.Body.Close()

	body, _ := ioutil.ReadAll(res.Body)

	var finobj resultBody
	json.Unmarshal([]byte(body), &finobj)

	fmt.Println(finobj.Predictions)

	return finobj.Predictions[0].Positive
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func master() {
	e := echo.New()

	var client = &http.Client{}

	e.GET("/compute", func(c echo.Context) error {
		text := c.QueryParam("text")

		words := strings.Split(text, ".")

		// MAPPING

		mapperHost := os.Getenv("MAPPER_HOST")

		var mapperIps []string
		ips, _ := net.LookupIP(mapperHost)
		for _, ip := range ips {
			mapperIps = append(mapperIps, ip.String())
		}

		mapSplitCount := int(math.Ceil(float64(len(words)) / float64(len(mapperIps))))

		var mapSplits = map[string][]string{}

		for idx, mapperIP := range mapperIps {
			if idx*mapSplitCount >= len(words) {
				break
			}
			mapSplits[mapperIP] = words[idx*mapSplitCount : min(idx*mapSplitCount+mapSplitCount, len(words))]
		}

		var mapping = map[string]map[string]int{}

		var wgm sync.WaitGroup
		wgm.Add(len(mapSplits))

		for host, split := range mapSplits {
			go func(host string, split []string) {
				defer wgm.Done()

				req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s:%s/map", host, os.Getenv("MAPPER_PORT")), nil)

				q := req.URL.Query()
				q.Add("str", strings.Join(split, " "))
				req.URL.RawQuery = q.Encode()

				res, _ := client.Do(req)
				body, _ := ioutil.ReadAll(res.Body)
				_ = res.Body.Close()

				buf := bytes.NewBuffer(body)

				var decodedMap map[string]int
				decoder := gob.NewDecoder(buf)
				_ = decoder.Decode(&decodedMap)

				mapping[host] = decodedMap
			}(host, split)
		}

		wgm.Wait()

		//SHUFFLING

		var shuffling = map[string][]int{}

		for _, host := range mapping {
			for word, count := range host {
				shuffling[word] = append(shuffling[word], count)
			}
		}

		//REDUCING

		reducerHost := os.Getenv("REDUCER_HOST")

		var reducerIps []string
		ips, _ = net.LookupIP(reducerHost)
		for _, ip := range ips {
			reducerIps = append(reducerIps, ip.String())
		}

		var shuffleWords []string
		for word := range shuffling {
			shuffleWords = append(shuffleWords, word)
		}

		reduceSplitCount := int(math.Ceil(float64(len(shuffleWords)) / float64(len(reducerIps))))

		var reduceSplits = map[string]map[string][]int{}

		for idx, reducerIP := range reducerIps {
			if idx*reduceSplitCount >= len(shuffleWords) {
				break
			}
			reduceWords := shuffleWords[idx*reduceSplitCount : min(idx*reduceSplitCount+reduceSplitCount, len(shuffleWords))]

			reduceSplits[reducerIP] = map[string][]int{}
			for _, reduceKey := range reduceWords {
				reduceSplits[reducerIP][reduceKey] = shuffling[reduceKey]
			}
		}

		var wgr sync.WaitGroup
		wgr.Add(len(reduceSplits))

		var reducing = map[string]map[string]int{}

		for host, split := range reduceSplits {
			go func(host string, split map[string][]int) {
				defer wgr.Done()
				req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s:%s/reduce", host, os.Getenv("REDUCER_PORT")), nil)

				buf := new(bytes.Buffer)
				encoder := gob.NewEncoder(buf)
				_ = encoder.Encode(split)

				q := req.URL.Query()
				q.Add("body", string(buf.Bytes()))
				req.URL.RawQuery = q.Encode()

				res, _ := client.Do(req)
				body, _ := ioutil.ReadAll(res.Body)
				_ = res.Body.Close()

				buf = bytes.NewBuffer(body)

				var decodedReduce = map[string]int{}
				decoder := gob.NewDecoder(buf)
				_ = decoder.Decode(&decodedReduce)

				reducing[host] = decodedReduce
			}(host, split)
		}

		wgr.Wait()

		return json.NewEncoder(c.Response()).Encode(&reducing)
	})

	e.Logger.Fatal(e.Start(":8080"))
}

func mapper() {
	//create object of HTTP server
	e := echo.New()

	//add request handler for route
	e.GET("/map", func(c echo.Context) error {

		//input paragraph from query params
		str := c.QueryParam("str")
		//split sentences by using dot to seperate them
		sentences := strings.Split(str, ".")
		//create a hash map for each sentence and the sentiment value in float
		mapping := map[string]float32{}

		for _, sentence := range sentences {
			mapping[sentence] = qHTTPRequests(sentence)
		}

		fmt.Println(mapping)

		//encode the mapping in bytes
		buf := new(bytes.Buffer)
		encoder := gob.NewEncoder(buf)
		encoder.Encode(mapping)

		//return bytes in http response
		return c.Blob(http.StatusOK, "application/octet-stream", buf.Bytes())
	})

	e.Logger.Fatal(e.Start(":8080"))
}

func reducer() {
	//create new instance of HTTP server
	e := echo.New()
	//add request handler
	e.GET("/reduce", func(c echo.Context) error {
		body := c.QueryParam("body")

		var reduceData = map[string]float32{}

		buf := bytes.NewBuffer([]byte(body))
		decoder := gob.NewDecoder(buf)
		decoder.Decode(&reduceData)

		var total float32
		for _, value := range reduceData {
			total += value
		}

		var lengthF float32
		lengthF = float32(len(reduceData))
		final := total / lengthF

		buf = new(bytes.Buffer)
		encoder := gob.NewEncoder(buf)
		encoder.Encode(final)

		return c.Blob(http.StatusOK, "application/octet-stream", buf.Bytes())
	})

	e.Logger.Fatal(e.Start(":8080"))
}
