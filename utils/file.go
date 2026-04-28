package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
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

func NewFileWatcher(location string) *FileWatcher {
	fw := FileWatcher{location: location}
	fw.Reset()
	return &fw
}

type FileWatcher struct {
	C <-chan os.FileInfo

	location string

	mtx      sync.Mutex
	init     atomic.Bool
	doneChan chan struct{}
	ticker   *time.Ticker
	lastInfo os.FileInfo
}

func (fw *FileWatcher) Reset() {

	fw.mtx.Lock()
	defer fw.mtx.Unlock()

	if !fw.init.CompareAndSwap(false, true) {
		return
	}

	fw.ticker = time.NewTicker(time.Second)
	fw.doneChan = make(chan struct{}, 1)
	signalChan := make(chan os.FileInfo, 1)

	go fw.watch(signalChan)

	fw.C = signalChan
}

func (fw *FileWatcher) watch(signalChan chan<- os.FileInfo) {

	for {

		select {

		case <-fw.ticker.C:

			info, _ := os.Stat(fw.location)
			if info == nil {
				continue
			} else if fw.lastInfo == nil {
				fw.lastInfo = info
				continue
			}

			if info.Size() == fw.lastInfo.Size() && info.ModTime().Equal(fw.lastInfo.ModTime()) {
				continue
			}

			fw.lastInfo = info

			select {
			case signalChan <- info:
			default:
			}

		case <-fw.doneChan:
			return
		}
	}
}

func (fw *FileWatcher) Stop() {

	fw.mtx.Lock()
	defer fw.mtx.Unlock()

	if !fw.init.CompareAndSwap(true, false) {
		return
	}

	fw.ticker.Stop()
	close(fw.doneChan)
}
