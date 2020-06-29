package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/parnurzeal/gorequest"
)

/*
json 解析模型
*/
type Movie struct {
	Cover    string `json:"cover"`
	CoverX   int    `json:"cover_x"`
	ID       string `json:"id"`
	IsNew    bool   `json:"is_new"`
	Playable bool   `json:"playable"`
	Rate     string `json:"rate"`
	Title    string `json:"title"`
	Url      string `json:"url"`
}

/*
json 解析模型
*/
type MovieResult struct {
	Movies []Movie `json:"subjects"`
}

/*
封面类型
*/
type Cover struct {
	Title string
	Data  []byte
}

/*
反序列化电影数据
*/
func UnmarshalMovies(movieJson string) ([]Movie, error) {
	movieResult := &MovieResult{}
	err := json.Unmarshal([]byte(movieJson), movieResult)
	if err != nil {
		return nil, err
	}
	return movieResult.Movies, nil
}

/*
获取电影数据
*/
func GetMovies(pageStart string) ([]Movie, error) {
	url := "https://movie.douban.com/j/search_subjects"
	_, body, errs := gorequest.New().Get(url).
		Param("type", "movie").
		Param("tag", "热门").
		Param("sort", "recommend").
		Param("page_limit", "20").
		Param("page_start", pageStart).
		Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:71.0) Gecko/20100101 Firefox/71.0").
		End()
	if errs != nil {
		return nil, errors.New("电影请求出错")
	}

	movies, err := UnmarshalMovies(body)
	if err != nil {
		return nil, err
	}
	return movies, nil
}

/*
下载封面数据
*/
func DownloadCover(movie Movie) (Cover, error) {
	resp, _, errs := gorequest.New().Get(movie.Cover).End()
	if errs != nil {
		return Cover{}, errors.New("图片下载出错")
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Cover{}, err
	}

	return Cover{Title: movie.Title, Data: body}, nil
}

/*
生成封面存储路径
*/
func coverName(coverTitle string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return path.Join(cwd, "douban", "covers", fmt.Sprintf("%s.jpg", coverTitle)), nil
}

/*
保存封面到本地
*/
func SaveCover(coverName string, coverData []byte) error {
	err := ioutil.WriteFile(coverName, coverData, 0777)
	if err != nil {
		return err
	}
	return nil
}

/*
电影数据 生产者
*/
func movieProducer(ch chan Movie, wg *sync.WaitGroup) {
	for i := 0; i < pages; i++ {
		wg.Add(1)
		go func(index int) {
			movies, err := GetMovies(strconv.Itoa(index * perPages))
			if err != nil {
				panic(err)
			}

			for _, movie := range movies {
				ch <- movie
			}
			wg.Done()
		}(i)
	}
	wg.Done()
}

/*
电影数据 消费者
封面数据 生产者
*/
func coverProducer(movieCH chan Movie, coverCH chan Cover, wg *sync.WaitGroup) {
	for movie := range movieCH {
		wg.Add(1)
		go func(m Movie) {
			cover, err := DownloadCover(m)
			if err != nil {
				panic(err)
			}
			coverCH <- cover
			wg.Done()
		}(movie)
	}
	wg.Done()
}

/*
保存封面 消费者
*/
func saveConsumer(coverCH chan Cover, saveWG *sync.WaitGroup) {
	for cover := range coverCH {
		saveWG.Add(1)
		go func(c Cover) {
			title := strings.ReplaceAll(c.Title, "/", "-")
			coverName, err := coverName(title)
			if err != nil {
				panic(err)
			}

			err = SaveCover(coverName, c.Data)
			if err != nil {
				panic(err)
			}

			fmt.Printf("%s\n", coverName)
			saveWG.Done()
		}(cover)
	}
	saveWG.Done()
}

/*
监控协程完成关闭信道
*/
func monitorMovie(ch chan Movie, wg *sync.WaitGroup) {
	wg.Wait()
	close(ch)
}

func monitorCover(ch chan Cover, wg *sync.WaitGroup) {
	wg.Wait()
	close(ch)
}

const pages = 20    // 需要抓取的页数
const perPages = 20 // 每页的条目数

func main() {
	start := time.Now()

	// 两个信道分别放电影数据和封面数据
	moviesChannel := make(chan Movie)
	coversChannel := make(chan Cover)

	// 控制多协程完成后关闭响应信道
	movieWG := sync.WaitGroup{}
	coverWG := sync.WaitGroup{}
	saveWG := sync.WaitGroup{}

	// 先加1 所有协程开始后减1 不然 monitor 检测 waitgroup 中无数据 将直接关闭信道
	// 生产者生产电影数据放入 movieChannel
	movieWG.Add(1)
	go movieProducer(moviesChannel, &movieWG)
	go monitorMovie(moviesChannel, &movieWG)

	// 消费者从 movieChannel 获取电影数据 同时生产封面数据放入 coverChannel
	coverWG.Add(1)
	go coverProducer(moviesChannel, coversChannel, &coverWG)
	go monitorCover(coversChannel, &coverWG)

	// 消费者从 coverChannel 获取数据保存到本地
	saveWG.Add(1)
	go saveConsumer(coversChannel, &saveWG)
	saveWG.Wait()

	fmt.Printf("done %s", time.Since(start).String())
}
