package main

import (
	"errors"
	"fmt"
	"github.com/cavaliergopher/grab/v3"
	"github.com/gythialy/magnet/constant"
	"github.com/nmmh/magneturi/magneturi"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	BestUrlFile = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"
	BestFile    = "trackers_best.txt"
)

// magnet:?xt=urn:btih:aad8482b43cca3342a9a56ee9064a836dd6c7195&dn=Enola.Holmes.2.2022.1080p.NF.WEB-DL.x265.10bit.HDR.DDP5.1.Atmos-SMURF&tr=http%3A%2F%2Ftracker.trackerfix.com%3A80%2Fannounce&tr=udp%3A%2F%2F9.rarbg.me%3A2990&tr=udp%3A%2F%2F9.rarbg.to%3A2930&tr=udp%3A%2F%2Ftracker.tallpenguin.org%3A15760&tr=udp%3A%2F%2Ftracker.thinelephant.org%3A12760
func main() {
	log.Printf("magnet %s @%s\n", constant.Version, constant.BuildTime)
	uri, err := magneturi.Parse(os.Args[1], true)
	if err != nil {
		log.Fatal(err)
	}
	filter, err := uri.Filter("xt", "dn", "tr")

	if err != nil {
		log.Fatal("invalid magnet url.")
	}

	dir, _ := os.Executable()
	f := filepath.Join(path.Dir(dir), BestFile)

	if s, err := os.Stat(f); errors.Is(err, os.ErrNotExist) || s.ModTime().Add(time.Hour*24).Before(time.Now()) {
		// create client
		client := grab.NewClient()
		req, _ := grab.NewRequest(f, BestUrlFile)

		// start download
		log.Printf("Downloading %v...\n", req.URL())
		resp := client.Do(req)
		log.Printf("%v\n", resp.HTTPResponse.Status)

		// start UI loop
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()

	Loop:
		for {
			select {
			case <-t.C:
				log.Printf("transferred %d / %d bytes (%.2f%%)\n",
					resp.BytesComplete(), resp.Size(), 100*resp.Progress())

			case <-resp.Done:
				// download is complete
				break Loop
			}
		}

		// check for errors
		if err := resp.Err(); err != nil {
			log.Fatalf("Download failed: %v\n", err)
		}

		log.Printf("%s saved to %s \n", BestFile, resp.Filename)
	}

	data, err := os.ReadFile(f)
	if err != nil {
		log.Fatal(err)
	}
	lines := regexp.MustCompile("\r?\n").Split(string(data), -1)
	sb := strings.Builder{}
	for _, line := range lines {
		if line != "" {
			sb.WriteString(fmt.Sprintf("&tr=%s", url.QueryEscape(line)))
		}
	}
	fmt.Println(filter.String() + sb.String())
}
