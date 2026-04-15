package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

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

	if ext := path.Ext(location); strings.EqualFold(ext, ".json") {
		if err := json.NewDecoder(file).Decode(&cfg); err != nil {
			return nil, fmt.Errorf("decode json config: %v", err)
		}
	} else if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode yaml config: %v", err)
	}

	return &cfg, nil
}
