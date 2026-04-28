package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func FindFileLocation(locations ...string) (string, error) {

	for _, path := range locations {
		stat, _ := os.Stat(path)
		if stat != nil && stat.Mode().IsRegular() {
			return path, nil
		}
	}

	return "", errors.New("no location exists")
}

func LoadConfigLocation[T any](location string) (*T, error) {

	file, err := os.Open(location)
	if err != nil {
		return nil, fmt.Errorf("open config: %v", err)
	}

	defer file.Close()

	var cfg T

	switch strings.ToLower(path.Ext(location)) {

	case ".json":
		if err := json.NewDecoder(file).Decode(&cfg); err != nil {
			return nil, fmt.Errorf("decode json config: %v", err)
		}

	case ".yml", ".yaml":
		if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
			return nil, fmt.Errorf("decode yaml config: %v", err)
		}

	default:
		return nil, errors.New("unsupported config file type")
	}

	return &cfg, nil
}

func WatchFile(location string) (<-chan os.FileInfo, context.CancelFunc) {

	ticker := time.NewTicker(time.Second)

	doneChan := make(chan struct{}, 1)

	signalChan := make(chan os.FileInfo, 1)

	var lastInfo os.FileInfo

	go func() {
		for {

			select {

			case <-ticker.C:

				info, _ := os.Stat(location)
				if info == nil {
					continue
				} else if lastInfo == nil {
					lastInfo = info
					continue
				}

				if info.Size() == lastInfo.Size() && info.ModTime().Equal(lastInfo.ModTime()) {
					continue
				}

				lastInfo = info

				select {
				case signalChan <- info:
				default:
				}

			case <-doneChan:
				return
			}
		}
	}()

	return signalChan, func() {
		ticker.Stop()
		close(doneChan)
		close(signalChan)
	}
}
