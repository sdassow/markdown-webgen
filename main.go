package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
	"github.com/sbertrang/atomic"
)

var quiet bool = false
var upre = regexp.MustCompile(`[A-Z]+`)

func htmlpath(mdpath string) string {
	// only change .md extension
	if mdpath[len(mdpath)-3:len(mdpath)] != ".md" {
		return mdpath
	}

	// only treat the filename
	dir, mdfile := path.Split(mdpath)

	// uppercase to lowercase
	x := upre.ReplaceAllStringFunc(mdfile, strings.ToLower)
	if x == "readme.md" {
		x = "index.md"
	}

	// underscores to dashes
	x = strings.ReplaceAll(x, "_", "-")

	// same direcory, new basename, new extension
	return path.Join(dir, x[0:len(x)-3]+".html")
}

func writeResult(tpl *template.Template, html string, file string, modtime time.Time) ([]byte, error) {
	data := struct {
		Body         template.HTML
		DateModified time.Time
	}{
		Body:         template.HTML(html),
		DateModified: modtime,
	}

	// get bytes and checksum at once
	b := bytes.NewBuffer([]byte{})
	h := sha1.New()
	w := io.MultiWriter(b, h)

	if err := tpl.ExecuteTemplate(w, tpl.Name(), &data); err != nil {
		return nil, err
	}
	srcsum := h.Sum(nil)

	// try to open target
	f, err := os.Open(file)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// compare if target exists
	if f != nil {
		h := sha1.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
		dstsum := h.Sum(nil)

		// all done when both are the same
		if bytes.Equal(dstsum, srcsum) {
			if !quiet {
				log.Printf("same checksum: %s (%s)", file, hex.EncodeToString(dstsum))
			}
			return dstsum, nil
		}
	}

	if !quiet {
		log.Printf("updating file: %s (%s)", file, hex.EncodeToString(srcsum))
	}

	if err := atomic.WriteFile(file, b, 0644); err != nil {
		return nil, err
	}

	return srcsum, nil
}

func copyFile(src, dst string) error {
	fin, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fin.Close()

	finstat, err := fin.Stat()
	if err != nil {
		return err
	}
	if !finstat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	buf := bytes.NewBuffer([]byte{})
	h := sha1.New()
	w := io.MultiWriter(buf, h)
	if _, err := io.Copy(w, fin); err != nil {
		return err
	}
	finsum := h.Sum(nil)

	// check destination
	fout, err := os.Open(dst)
	// it's okay for files to not exist, the rest is not
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// get previous checksum
	if fout != nil {
		defer fout.Close()
		h := sha1.New()
		if _, err := io.Copy(h, fout); err != nil {
			return err
		}
		foutsum := h.Sum(nil)

		// same checksum, we're good
		if bytes.Equal(foutsum, finsum) {
			if !quiet {
				log.Printf("same checksum: %s (%s)", dst, hex.EncodeToString(finsum))
			}
			return nil
		}
	}

	if !quiet {
		log.Printf("updating file: %s (%s)", dst, hex.EncodeToString(finsum))
	}

	if err := atomic.WriteFile(dst, buf, 0644); err != nil {
		return err
	}

	return nil
}

type MarkdownFile struct {
	Content      []byte
	Path         string
	Dir          string
	Name         string
	DateModified time.Time
}

type AssetFile struct {
	Path string
	Info os.FileInfo
}

func usage() {
	progname, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	_, progname = path.Split(progname)
	fmt.Fprintf(os.Stderr, "usage: %s [-destdir dir] [-assetdir dir] [-tmplfile file] md [md ...]\n", progname)
	os.Exit(1)
}

func main() {
	var destdir string
	var assetdir string
	var tmplfile string

	flag.StringVar(&destdir, "destdir", "", "destination directory for output files")
	flag.StringVar(&assetdir, "assetdir", "", "asset source directory")
	flag.StringVar(&tmplfile, "tmplfile", "template.html", "html template")
	flag.BoolVar(&quiet, "quiet", false, "hide detailed log output")
	flag.Parse()

	files := make([]string, 0)
	data := make(map[string]MarkdownFile)
	xmap := make(map[string]string)

	args := flag.Args()
	files = append(files, args...)

	tpl, err := template.ParseFiles(tmplfile)
	if err != nil {
		log.Fatal(err)
	}

	if len(files) == 0 {
		usage()
	}

	if !quiet {
		log.Println("reading input...")
	}

	re := regexp.MustCompile("([^()]+\\.md)")
	for n := 0; n < len(files); n++ {
		fpath := files[n]
		dir, file := path.Split(fpath)

		// XXX: subdir check

		_, found := data[fpath]
		if found {
			continue
		}
		xmap[file] = fpath

		content, err := ioutil.ReadFile(fpath)
		if err != nil {
			log.Fatal(err)
		}
		fi, err := os.Stat(fpath)
		if err != nil {
			log.Fatal(err)
		}
		data[fpath] = MarkdownFile{
			Content:      content,
			Path:         fpath,
			Dir:          dir,
			Name:         file,
			DateModified: fi.ModTime(),
		}

		datafiles := re.FindAllString(string(content), -1)

		for _, datafile := range datafiles {
			files = append(files, path.Join(dir, datafile))
		}

		if !quiet {
			log.Printf("found markdown: %s", file)
		}
	}

	files = nil
	for k := range data {
		files = append(files, k)
	}
	sort.Strings(files)

	hre := regexp.MustCompile(`href="[^"]+\.md"`)

	if !quiet {
		log.Println("generating html...")
	}

	renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		Flags: blackfriday.CommonHTMLFlags | blackfriday.HrefTargetBlank,
	})
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")

	for _, file := range files {
		unsafe := blackfriday.Run([]byte(data[file].Content),
			blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.NoEmptyLineBeforeBlock|blackfriday.AutoHeadingIDs),
			blackfriday.WithRenderer(renderer),
		)
		html := p.SanitizeBytes(unsafe)

		newhtml := hre.ReplaceAllStringFunc(string(html), func(in string) string {
			f := in[6 : len(in)-1]
			return `href="` + htmlpath(f) + `"`
		})

		destpath := htmlpath(file)
		if destdir != "" {
			_, base := path.Split(destpath)
			destpath = path.Join(destdir, base)
		}

		_, err := writeResult(tpl, newhtml, destpath, data[file].DateModified)
		if err != nil {
			log.Fatal(err)
		}
	}

	// all done without assets to copy
	if assetdir == "" {
		return
	}

	if !quiet {
		log.Printf("scanning for assets: %s\n", assetdir)
	}

	assets := make([]AssetFile, 0)
	if err := filepath.Walk(assetdir, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name()[0:1] == "." || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(assetdir, file)
		if err != nil {
			log.Fatal(err)
		}
		assets = append(assets, AssetFile{
			Path: rel,
			Info: info,
		})
		return nil
	}); err != nil {
		log.Fatal(err)
	}

	for _, asset := range assets {
		dst := path.Join(destdir, asset.Path)
		dir, _ := path.Split(dst)
		if dir != destdir {
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Fatal(err)
			}
		}

		src := path.Join(assetdir, asset.Path)
		if err := copyFile(src, dst); err != nil {
			log.Fatal(err)
		}
	}

}
