package asset_packager

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"github.com/fsnotify/fsnotify"
)

type Meta struct {
	ResourcesRequested []string
	ResourcesFailed []string
}

type Packager struct {
	AssetDirectory string
	AssetMap map[string]bool
	Watcher *fsnotify.Watcher
}

func New(asset_dir string) (*Packager, error) {
	asset_dir, _ = filepath.Abs(asset_dir)
	info, err := os.Stat(asset_dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", asset_dir)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	toret := &Packager{
		AssetDirectory : asset_dir,
		AssetMap : map[string]bool{},
		Watcher : watcher,
	}
	filepath.Walk(asset_dir, func(path string, info os.FileInfo, err error) error {
		asset, _ := filepath.Rel(asset_dir, path)
		toret.Watcher.Add(path)
		if !info.IsDir() {
			toret.AssetMap[asset] = true
		}
		return nil
	})
	go func(p *Packager) {
		for event := range(p.Watcher.Events) {
			assetname, _ := filepath.Rel(p.AssetDirectory, event.Name)
			switch (event.Op) {
			case fsnotify.Create:
				p.AssetMap[assetname] = true
			case fsnotify.Remove, fsnotify.Rename:
				p.AssetMap[assetname] = false
			}
		}
	}(toret)
	go func(p *Packager) {
		for err := range(p.Watcher.Errors) {
			log.Println(err)
		}
	}(toret)
	fmt.Printf("%v\n", toret.AssetMap)
	return toret, nil
}

func (p *Packager) Package(dst io.Writer, prefix string, filenames ...string) {
	gWriter := gzip.NewWriter(dst)
	defer gWriter.Close()

	tWriter := tar.NewWriter(gWriter)
	defer tWriter.Close()

	meta := Meta {
		ResourcesRequested : []string{},
		ResourcesFailed: []string{},
	}

	for _, filename := range(filenames) {
		meta.ResourcesRequested = append(meta.ResourcesRequested, filename)
		if !p.AssetMap[filename] {
			fmt.Printf("Resource Request Failed: Resource [%s] not in AssetMap\n", filename)
			meta.ResourcesFailed = append(meta.ResourcesFailed, filename)
			continue
		}
		abs_filename := filepath.Join(p.AssetDirectory, filename)
		info, err := os.Stat(abs_filename)
		if err != nil {
			fmt.Printf("Resource Request Failed: %v\n", err)
			meta.ResourcesFailed = append(meta.ResourcesFailed, filename)
			continue
		}
		thdr := &tar.Header {
			Name : path.Join(prefix, filename),
			Mode : int64(info.Mode()),
			Size : info.Size(),
		}
		file, err := os.Open(abs_filename)
		if err != nil {
			fmt.Printf("Resource Request Failed: %v\n", err)
			meta.ResourcesFailed = append(meta.ResourcesFailed, filename)
			continue
		}
		if tar_err := tWriter.WriteHeader(thdr); err != nil {
			log.Fatalln(tar_err)
		}
		if _, cp_err := io.Copy(tWriter, file); err != nil {
			log.Fatalln(cp_err)
		}
		file.Close()
	}
	mbytes, err := json.MarshalIndent(meta, "", "\t")
	if err != nil {
		log.Fatalln("Meta to JSON:", err)
		return
	}
	mhdr := &tar.Header {
		Name : path.Join(prefix, "metadata.json"),
		Mode : 0600,
		Size : int64(len(mbytes)),
	}
	if hd_err := tWriter.WriteHeader(mhdr); hd_err != nil {
		log.Fatalln("Meta to JSON:", hd_err)
	}
	if _, w_err := tWriter.Write(mbytes); w_err != nil {
		log.Fatalln("Meta to JSON:", w_err)
	}
}

func (p *Packager) HttpPackage(w http.ResponseWriter, req *http.Request) {
	assets := []string{}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Encoding", "gzip")
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Println(err)
		return
	}
	defer req.Body.Close()
	err = json.Unmarshal(body, &assets)
	if err != nil {
		log.Println(err)
		return
	}
	p.Package(w, "", assets...)
}

func (p *Packager) Close() {
	p.Watcher.Close()
}
