package main

import (
	//"path/filepath"
	"log"
	"bytes"
	"io/ioutil"
	"path"
	"flag"
	"regexp"
	"strings"
	"sort"
	"html/template"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
	"github.com/sbertrang/atomic"
)

var uchars = regexp.MustCompile(`[A-Z]+`)

func htmlpath(mdpath string) string {
	if mdpath[len(mdpath)-3:len(mdpath)] != ".md" {
		return mdpath
	}

	dir, mdfile := path.Split(mdpath)
	x := uchars.ReplaceAllStringFunc(mdfile, strings.ToLower)
	if x == "readme.md" {
		x = "index.md"
	}

	return path.Join(dir, x[0:len(x)-3] + ".html")
}

func writeResult(tpl *template.Template, html string, file string) error {
	buf := bytes.NewBuffer([]byte{})
	data := struct{
		Body template.HTML
	}{
		Body: template.HTML(html),
	}

	if err := tpl.ExecuteTemplate(buf, "template.html", &data); err != nil {
		return err
	}

	if err := atomic.WriteFile(file, buf); err != nil {
		return err
	}

	return nil
}

type MarkdownFile struct {
	Content []byte
	Path	string
	Dir	string
	Name	string
}

func main() {
	var destdir string
	var tmplfile string

	flag.StringVar(&destdir, "destdir", "", "destination directory for output files")
	flag.StringVar(&tmplfile, "tmplfile", "template.html", "html template")
	flag.Parse()


	files := make([]string, 0)
	data := make(map[string]MarkdownFile)
	xmap := make(map[string]string)

	args := flag.Args()
	files = append(files, args...)

	//tmplfile := "template.html"
	tpl, err := template.New("template.html").ParseFiles(tmplfile)
	if err != nil {
		log.Fatal(err)
	}

	re := regexp.MustCompile("([^()]+\\.md)")
	for n := 0; n < len(files); n++ {
		fpath := files[n]
		dir, file := path.Split(fpath)

		// XXX: subdir check

		_, found := data[fpath]
		if found {
			log.Printf("saw %s already, skipping", fpath)
			continue
		}
		xmap[file] = fpath

		content, err := ioutil.ReadFile(fpath)
		if err != nil {
			log.Fatal(err)
		}
		data[fpath] = MarkdownFile{
			Content: content,
			Path: fpath,
			Dir: dir,
			Name: file,
		}

		datafiles := re.FindAllString(string(content), -1)

		for _, datafile := range datafiles {
			files = append(files, path.Join(dir, datafile))
		}

		log.Printf("found markdown file: %s", file)
	}

	files = nil
	for k := range data {
		files = append(files, k)
	}
	sort.Strings(files)

	hre := regexp.MustCompile(`href="[^"]+\.md"`)

	for _, file := range files {
		unsafe := blackfriday.Run([]byte(data[file].Content))
		html := bluemonday.UGCPolicy().SanitizeBytes(unsafe)

		newhtml := hre.ReplaceAllStringFunc(string(html), func(in string) string {
			//log.Printf("repl: %s", in)
			f := in[6:len(in)-1]
			return `href="` + htmlpath(f) + `"`
		})

		destpath := htmlpath(file)
		if destdir != "" {
			_, base := path.Split(destpath)
			destpath = path.Join(destdir, base)
		}

		log.Printf("writing result: %s", destpath)

		if err := writeResult(tpl, newhtml, destpath); err != nil {
			log.Fatal(err)
		}
	}

/*

	log.Printf("walk dir: %s\n", dir)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		//if info.IsDir() {
		//	return filepath.SkipDir
		//}
		files = append(files, path)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, info := range files {
		log.Printf("info: %+v", info)
	}

*/


}
