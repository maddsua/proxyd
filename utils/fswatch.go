package utils

import (
	"context"
	"os"
	"time"
)

func WatchFile(name string) (<-chan os.FileInfo, context.CancelFunc) {

	ticker := time.NewTicker(time.Second)

	doneChan := make(chan struct{}, 1)

	signalChan := make(chan os.FileInfo, 1)

	var lastInfo os.FileInfo

	go func() {
		for {

			select {

			case <-ticker.C:

				info, _ := os.Stat(name)
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
