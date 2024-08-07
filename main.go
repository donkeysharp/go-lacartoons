package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

/**
 * TODO:
 * - Abstract into a scrapper service struct
 * - Refactor fetching urls, make it generic
 * - Add a persistence layer using sqlite
 * - When retrieving per episode information add a worker pool for concurrency, use channels
 * - Add a downloading service, it should as well support concurrency with worker pool
 *   - Make sure youtube-dl or yt-dlp exist
 *   - Add some sort of concurrency, max of 3 or 5 maybe
 *
 */

func getLastPage(url string) (int, error) {
	res, err := http.Get(url)
	if err != nil {
		return 0, err
	}

	defer res.Body.Close()
	if res.StatusCode != 200 {
		return 0, fmt.Errorf("GET request failed with status code %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return 0, err
	}

	var lastPage int
	var retErr error = nil

	// Get the last-1 item in the ul to get the last page
	doc.Find(".paginacion-all-series ul.pagination nav ul.pagination li:nth-last-child(2) a").EachWithBreak(func(i int, s *goquery.Selection) bool {
		html, err := s.Html()
		if err != nil {
			return true
		}
		lastPage, retErr = strconv.Atoi(html)
		// break as there there is only to be one item
		return true
	})
	return lastPage, retErr
}

func getAllSeriesUrls(url string) ([]string, error) {
	lastPage, err := getLastPage(url)
	if err != nil {
		return nil, err
	}
	urls := make([]string, lastPage)
	for page := 1; page <= lastPage; page++ {
		urls[page-1] = fmt.Sprintf("%s/?page=%d", url, page)
	}
	return urls, nil
}

type TVShow struct {
	Name     string
	Marker   string
	Url      string
	ImageUrl string
	Year     int
	Rate     int
	Seasons  []*Season
}

type Episode struct {
	Name        string
	InternalUrl string
	ExternalUrl string
	Chapter     int
}

type Season struct {
	Name     string
	Episodes []*Episode
}

func (s *TVShow) String() string {
	return fmt.Sprintf("Name: %v Marker: %v Url: %v Year: %v Image Url: %v", s.Name, s.Marker, s.Url, s.Year, s.ImageUrl)
}

func getSeries(rawUrl string) ([]*TVShow, error) {
	res, err := http.Get(rawUrl)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GET request failed with status code %d", res.StatusCode)
	}

	url, err := url.Parse(rawUrl)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var tvShows []*TVShow = make([]*TVShow, 0)
	var returnErr error = nil

	doc.Find(".conjuntos-series a").Each(func(i int, s *goquery.Selection) {
		var tvShow *TVShow = &TVShow{}
		imgUrlEl := s.Find("img").First()
		if imgUrlEl != nil {
			if src, exists := imgUrlEl.Attr("src"); exists {
				tvShow.ImageUrl = fmt.Sprintf("%v://%v%v", url.Scheme, url.Host, src)
			}
		}
		nameEl := s.Find(".informacion-serie div p.nombre-serie").First()
		if nameEl != nil {
			tvShow.Name = nameEl.Text()
		}
		markerEl := s.Find(".informacion-serie div span.marcador-cartoon").First()
		if markerEl != nil {
			tvShow.Marker = strings.Trim(markerEl.Text(), " \n\t")
		}
		yearEl := s.Find(".informacion-serie div span.marcador-ano").First()
		if yearEl != nil {
			year := 0
			year, _ = strconv.Atoi(yearEl.Text())
			tvShow.Year = year
		}
		rateEl := s.Find(".informacion-serie div span.valoracion").First()
		if rateEl != nil {
			rate := 0
			rate, _ = strconv.Atoi(rateEl.Text())
			tvShow.Rate = rate
		}
		showURL, exists := s.Attr("href")
		if exists {
			tvShow.Url = fmt.Sprintf("%v://%v%v", url.Scheme, url.Host, showURL)
			tvShows = append(tvShows, tvShow)
		}
	})

	return tvShows, returnErr
}

func getEpisodesByShow(tvShow *TVShow, baseUrl string) ([]*Season, error) {
	res, err := http.Get(tvShow.Url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GET request failed with status code %d", res.StatusCode)
	}

	url, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var seasons []*Season = make([]*Season, 0)
	var retErr error = nil
	doc.Find(".contenedor-episondios h4.estilo-temporada").Each(func(i int, s *goquery.Selection) {
		seasonName := strings.Trim(s.Text(), " \t\n")
		season := &Season{
			Name:     seasonName,
			Episodes: make([]*Episode, 0),
		}
		seasons = append(seasons, season)
	})

	doc.Find(".contenedor-episondios h4.estilo-temporada + div ul").Each(func(seasonIndex int, s *goquery.Selection) {
		s.Find("li a").Each(func(episodeIndex int, s1 *goquery.Selection) {
			episodeName := strings.Trim(s1.Text(), " \t\n")
			episodePath, _ := s1.Attr("href")
			internalUrl := fmt.Sprintf("%v://%v%v", url.Scheme, url.Host, episodePath)
			externalUrl, err := getEpisodeExternalUrl(internalUrl)
			if err != nil {
				retErr = err
				return
			}

			episode := &Episode{
				Chapter:     episodeIndex + 1,
				Name:        episodeName,
				InternalUrl: internalUrl,
				ExternalUrl: externalUrl,
			}

			seasons[seasonIndex].Episodes = append(seasons[seasonIndex].Episodes, episode)
		})

	})

	return seasons, retErr
}

func getEpisodeExternalUrl(internalUrl string) (string, error) {
	res, err := http.Get(internalUrl)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("GET request failed with status code %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", err
	}
	s := doc.Find(".container iframe").First()
	externalUrl, _ := s.Attr("src")

	return externalUrl, nil
}

func getSeriesPerPage(urls []string) ([]*TVShow, error) {
	var tvShows []*TVShow = make([]*TVShow, 0)
	for _, url := range urls {
		series, err := getSeries(url)
		if err != nil {
			return nil, err
		}
		tvShows = append(tvShows, series...)
	}
	return tvShows, nil
}

func main() {
	var rawUrl string = "https://www.lacartoons.com"
	urls, err := getAllSeriesUrls(rawUrl)
	if err != nil {
		fmt.Println(err)
	}
	tvShows, err := getSeriesPerPage(urls)
	if err != nil {
		fmt.Println(err)
	}
	for i, show := range tvShows {
		fmt.Printf("%2d %s %s\n", i+1, show.Name, show.Url)
		seasons, err := getEpisodesByShow(show, rawUrl)
		if err != nil {
			fmt.Println("ERROR")
			fmt.Println(err)
		}
		for j, season := range seasons {
			fmt.Printf("\t%2d %s %d\n", j+1, season.Name, len(season.Episodes))
			for _, episode := range season.Episodes {
				fmt.Printf("\t\t%d <-> %s <-> %s <-> %s\n", episode.Chapter, episode.Name, episode.InternalUrl, episode.ExternalUrl)
			}
		}

		// TODO: remove this
		if i == 3 {
			break
		}
	}
}
