package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

const (
	archiveUrl = `https://my.atlassian.com/download/feeds/archived/crowd.json`
	currentUrl = `https://my.atlassian.com/download/feeds/current/crowd.json`
	eapUrl     = `https://my.atlassian.com/download/feeds/eap/crowd.json`
)

var tmpl = template.Must(template.ParseFiles("Dockerfile.tmpl"))

func main() {
	versionDirs, err := getDirs(".")
	if err != nil {
		fmt.Println("error fetching version dirs:", err)
		os.Exit(1)
	}

	versions, err := getVersions(currentUrl, archiveUrl, eapUrl)
	if err != nil {
		fmt.Println("error reading atlassian feeds:", err)
		os.Exit(1)
	}

	for _, dir := range versionDirs {
		p, ok := versions[dir]
		if !ok {
			fmt.Println("can't find url for version", dir)
			os.Exit(1)
		}

		if err := update(dir, p); err != nil {
			fmt.Printf("error updating %s: %s\n", dir, err)
			os.Exit(1)
		}
	}
}

func update(dir string, pkg Package) (err error) {
	f, err := os.OpenFile(filepath.Join(dir, "Dockerfile"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tmpl.Execute(f, pkg); err != nil {
		return err
	}
	err = copyFile("docker-entrypoint.sh", filepath.Join(dir, "docker-entrypoint.sh"), 0764)
	if err != nil {
		return err
	}
	// If the file already existed the permissions might not be correct to run
	// inside the container.
	return os.Chmod(filepath.Join(dir, "docker-entrypoint.sh"), 0764)
}

func getDirs(path string) (dirs []string, err error) {
	entries, err := ioutil.ReadDir(".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs, nil
}

// getVersions gets the latest packages from the feeds and marks any from the
// latestFeed as Latest.
func getVersions(latestFeed string, otherFeeds ...string) (versions map[string]Package, err error) {
	versions = map[string]Package{}

	for _, url := range append(otherFeeds, latestFeed) {
		newVersions, err := fetchLatestTarVersions(url)
		if err != nil {
			return nil, err
		}
		for v, p := range newVersions {
			if url == latestFeed {
				p.Latest = true
			}
			versions[v] = p
		}
	}
	return versions, nil
}

// fetchLatestTarVersions reads the atlassian download feed and fetches the
// latest tar.gz entry for each version.
func fetchLatestTarVersions(url string) (versions map[string]Package, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	start := bytes.Index(data, []byte("("))
	end := bytes.LastIndex(data, []byte(")"))
	if !(end > start && start > -1) {
		return nil, errors.New("error in jsonp content")
	}
	var archives []Package
	err = json.Unmarshal(data[start+1:end], &archives)
	if err != nil {
		return nil, err
	}
	versions = map[string]Package{}
	for _, archive := range archives {
		filename := path.Base(archive.ZipURL)
		if !strings.Contains(filename, ".tar.gz") ||
			(strings.Contains(filename, "enterprise") && !strings.Contains(filename, "standalone")) ||
			strings.Contains(filename, "cluster") ||
			strings.Contains(filename, "war") {
			continue
		}
		majmin := archive.Version.MajorMinor()
		v, ok := versions[majmin]
		if !ok || time.Time(archive.Released).After(time.Time(v.Released)) {
			versions[majmin] = archive
		}
	}
	return versions, nil
}

type Package struct {
	ZipURL   string        `json:"zipUrl"`
	Version  Version       `json:"version"`
	Released AtlassianTime `json:"released"`
	Latest   bool
}

type Version string

var versionSeparator = regexp.MustCompile(`(\.|-)`)

func (v Version) MajorMinor() string {
	parts := versionSeparator.Split(string(v), 3)
	if len(parts) < 2 {
		return "0.0"
	}
	return parts[0] + "." + parts[1]
}

type AtlassianTime time.Time

func (a *AtlassianTime) UnmarshalJSON(data []byte) error {
	var str string
	err := json.Unmarshal(data, &str)
	if err != nil {
		return err
	}
	t, err := time.Parse("02-Jan-2006", str)
	if err != nil {
		return err
	}
	*a = AtlassianTime(t)
	return nil
}

func copyFile(src, dst string, perm os.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() {
		oErr := out.Close()
		if err == nil {
			err = oErr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
