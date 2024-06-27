package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	baseURL      = "https://api.wordpress.org/plugins/info/1.2/?action=query_plugins&request[page]=%d"
	minInstalls  = 1000
	maxWorkers   = 5
	requestRetry = 3
	rateLimit    = time.Second / 2 // 2 requests per second
)

type Plugin struct {
	Slug           string `json:"slug"`
	Version        string `json:"version"`
	DownloadLink   string `json:"download_link"`
	ActiveInstalls int    `json:"active_installs"`
}

type PluginList struct {
	Plugins []Plugin `json:"plugins"`
}

func fetchPluginList(pageNumber int) (PluginList, error) {
	var pluginList PluginList
	url := fmt.Sprintf(baseURL, pageNumber)

	for i := 0; i < requestRetry; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			err = json.NewDecoder(resp.Body).Decode(&pluginList)
			if err == nil {
				return pluginList, nil
			}
		}
		time.Sleep(rateLimit)
	}

	return pluginList, fmt.Errorf("failed to fetch page %d after %d retries", pageNumber, requestRetry)
}

func downloadPlugin(plugin Plugin, wg *sync.WaitGroup) {
	defer wg.Done()

	resp, err := http.Get(plugin.DownloadLink)
	if err != nil {
		fmt.Printf("Failed to download %s (%s): %v\n", plugin.Slug, plugin.DownloadLink, err)
		return
	}
	defer resp.Body.Close()

	fileName := fmt.Sprintf("%s-%s.zip", plugin.Slug, plugin.Version)
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Printf("Failed to create file %s: %v\n", fileName, err)
		return
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		fmt.Printf("Failed to write to file %s: %v\n", fileName, err)
		return
	}

	fmt.Printf("Downloaded %s version %s\n", plugin.Slug, plugin.Version)
}

func main() {
	var wg sync.WaitGroup
	jobs := make(chan Plugin, maxWorkers*2) // give some buffer to the channel

	var rateLimiter = time.Tick(rateLimit)

	for i := 0; i < maxWorkers; i++ {
		go func() {
			for plugin := range jobs {
				downloadPlugin(plugin, &wg)
			}
		}()
	}

	pageNumber := 1

	go func() {
		for {
			pluginList, err := fetchPluginList(pageNumber)
			if err != nil {
				fmt.Printf("Failed to fetch plugin list for page %d: %v\n", pageNumber, err)
				return
			}

			if len(pluginList.Plugins) == 0 {
				break
			}

			for _, plugin := range pluginList.Plugins {
				if plugin.ActiveInstalls >= minInstalls {
					<-rateLimiter
					wg.Add(1)
					jobs <- plugin
				}
			}
			pageNumber++
		}
		close(jobs)
	}()

	wg.Wait()
}
