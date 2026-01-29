package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	IndexerURL    string        `yaml:"indexerURL"`
	QueryInterval time.Duration `yaml:"queryInterval"`
	StallTimeout  time.Duration `yaml:"stallTimeout"`
	RestartSleep  time.Duration `yaml:"restartSleep"`
	ContainerName string        `yaml:"containerName"`
	MetricName    string        `yaml:"metricName"`
}

type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func main() {
	config, err := LoadConfig("config")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting near-lake-supervisor")
	log.Printf("Indexer URL: %s", config.IndexerURL)
	log.Printf("Query Interval: %v", config.QueryInterval)
	log.Printf("Stall Timeout: %v", config.StallTimeout)
	log.Printf("Container: %s", config.ContainerName)

	var lastBlockHeight int64 = -1
	var lastProgressTime time.Time = time.Now()
	var isRestarting bool = false

	ticker := time.NewTicker(config.QueryInterval)
	defer ticker.Stop()

	// Initial query
	blockHeight, err := queryBlockHeight(config)
	if err != nil {
		log.Printf("Warning: Failed to query block height: %v", err)
	} else {
		lastBlockHeight = blockHeight
		lastProgressTime = time.Now()
		log.Printf("Initial block height: %d", blockHeight)
	}

	for range ticker.C {
		if isRestarting {
			log.Printf("Still in restart cooldown period, skipping query")
			continue
		}

		blockHeight, err := queryBlockHeight(config)
		if err != nil {
			log.Printf("Error querying block height: %v", err)
			// Check if we should restart due to query failures
			if time.Since(lastProgressTime) > config.StallTimeout {
				log.Printf("Block height query has been failing for %v, attempting restart", config.StallTimeout)
				if err := restartContainer(config); err != nil {
					log.Printf("Error restarting container: %v", err)
				} else {
					isRestarting = true
					lastProgressTime = time.Now()
					go func() {
						time.Sleep(config.RestartSleep)
						isRestarting = false
						log.Printf("Restart cooldown complete, resuming monitoring")
					}()
				}
			}
			continue
		}

		log.Printf("Current block height: %d (last: %d)", blockHeight, lastBlockHeight)

		if blockHeight > lastBlockHeight {
			// Block height is progressing
			lastBlockHeight = blockHeight
			lastProgressTime = time.Now()
			log.Printf("Block height progressing: %d", blockHeight)
		} else if blockHeight == lastBlockHeight {
			// Block height is stalled
			stallDuration := time.Since(lastProgressTime)
			log.Printf("Block height stalled at %d for %v", blockHeight, stallDuration)

			if stallDuration > config.StallTimeout {
				log.Printf("Block height has been stalled for %v (threshold: %v), restarting container", stallDuration, config.StallTimeout)
				if err := restartContainer(config); err != nil {
					log.Printf("Error restarting container: %v", err)
				} else {
					isRestarting = true
					lastProgressTime = time.Now()
					go func() {
						time.Sleep(config.RestartSleep)
						isRestarting = false
						log.Printf("Restart cooldown complete, resuming monitoring")
					}()
				}
			}
		} else {
			// Block height decreased (shouldn't happen, but handle it)
			log.Printf("Warning: Block height decreased from %d to %d", lastBlockHeight, blockHeight)
			lastBlockHeight = blockHeight
			lastProgressTime = time.Now()
		}
	}
}

func queryBlockHeight(config Config) (int64, error) {
	// Try Prometheus API first (JSON format)
	url := fmt.Sprintf("%s/api/v1/query?query=%s", config.IndexerURL, config.MetricName)
	resp, err := http.Get(url)
	if err != nil {
		// Fallback to metrics endpoint (text format)
		return queryBlockHeightText(config)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fallback to metrics endpoint
		return queryBlockHeightText(config)
	}

	var promResp PrometheusResponse
	if err := json.NewDecoder(resp.Body).Decode(&promResp); err != nil {
		// Fallback to metrics endpoint
		return queryBlockHeightText(config)
	}

	if promResp.Status != "success" || len(promResp.Data.Result) == 0 {
		// Fallback to metrics endpoint
		return queryBlockHeightText(config)
	}

	// Extract value from Prometheus response
	valueStr, ok := promResp.Data.Result[0].Value[1].(string)
	if !ok {
		return queryBlockHeightText(config)
	}

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return queryBlockHeightText(config)
	}

	return int64(value), nil
}

func queryBlockHeightText(config Config) (int64, error) {
	url := fmt.Sprintf("%s/metrics", config.IndexerURL)
	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, config.MetricName) {
			// Parse Prometheus text format: metric_name value
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				value, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					continue
				}
				return int64(value), nil
			}
		}
	}

	return 0, fmt.Errorf("metric %s not found in response", config.MetricName)
}

func restartContainer(config Config) error {
	if config.ContainerName == "" {
		return fmt.Errorf("container name not specified")
	}

	log.Printf("Restarting container: %s", config.ContainerName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "restart", config.ContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker restart failed: %w, output: %s", err, string(output))
	}
	log.Printf("docker restart output: %s", string(output))
	return nil
}

func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("local")
	viper.SetConfigType("yaml")

	// Set defaults
	viper.SetDefault("indexerURL", "http://indexer:3030")
	viper.SetDefault("queryInterval", "30s")
	viper.SetDefault("stallTimeout", "5m")
	viper.SetDefault("restartSleep", "900s")
	viper.SetDefault("metricName", "near_indexer_streaming_current_block_height")
	viper.SetDefault("containerName", "near-lake-indexer")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		// If config file doesn't exist, use defaults
		log.Printf("Config file not found, using defaults: %v", err)
	}

	err = viper.Unmarshal(&config)
	if err != nil {
		return
	}

	// Parse duration strings
	if queryIntervalStr := viper.GetString("queryInterval"); queryIntervalStr != "" {
		if d, err := time.ParseDuration(queryIntervalStr); err == nil {
			config.QueryInterval = d
		}
	}
	if stallTimeoutStr := viper.GetString("stallTimeout"); stallTimeoutStr != "" {
		if d, err := time.ParseDuration(stallTimeoutStr); err == nil {
			config.StallTimeout = d
		}
	}
	if restartSleepStr := viper.GetString("restartSleep"); restartSleepStr != "" {
		if d, err := time.ParseDuration(restartSleepStr); err == nil {
			config.RestartSleep = d
		}
	}

	return
}
