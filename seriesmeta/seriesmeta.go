package main

import (
	"archive/zip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/geek1011/koboutils/kobo"

	"github.com/beevik/etree"
	"github.com/mattn/go-zglob"
	"golang.org/x/tools/godoc/vfs/zipfs"

	"github.com/spf13/pflag"

	_ "github.com/mattn/go-sqlite3"
)

var version = "dev"

func helpExit() {
	fmt.Fprintf(os.Stderr, "Usage: seriesmeta [OPTIONS] [KOBO_PATH]\n\nVersion:\n  seriesmeta %s\n\nOptions:\n", version)
	pflag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nArguments:\n  KOBO_PATH is the path to the Kobo eReader. If not specified, seriesmeta will try to automatically detect the Kobo.\n")
	if runtime.GOOS == "windows" {
		time.Sleep(time.Second * 2)
	}
	os.Exit(1)
}

func errExit() {
	if runtime.GOOS == "windows" {
		time.Sleep(time.Second * 2)
	}
	os.Exit(1)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

// pathToContentID gets the content ID for a book. The path needs to be relative to the root of the kobo.
func pathToContentID(relpath string) string {
	return fmt.Sprintf("file:///mnt/onboard/%s", filepath.ToSlash(relpath))
}

func contentIDToImageID(contentID string) string {
	imageID := contentID

	imageID = strings.Replace(imageID, " ", "_", -1)
	imageID = strings.Replace(imageID, "/", "_", -1)
	imageID = strings.Replace(imageID, ":", "_", -1)
	imageID = strings.Replace(imageID, ".", "_", -1)

	return imageID
}

func getMeta(path string) (string, float64, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", 0, err
	}

	zfs := zipfs.New(zr, "epub")
	rsk, err := zfs.Open("/META-INF/container.xml")
	if err != nil {
		return "", 0, err
	}
	defer rsk.Close()

	container := etree.NewDocument()
	_, err = container.ReadFrom(rsk)
	if err != nil {
		return "", 0, err
	}

	rootfile := ""
	for _, e := range container.FindElements("//rootfiles/rootfile[@full-path]") {
		rootfile = e.SelectAttrValue("full-path", "")
	}

	if rootfile == "" {
		return "", 0, errors.New("Cannot parse container")
	}

	rrsk, err := zfs.Open("/" + rootfile)
	if err != nil {
		return "", 0, err
	}
	defer rrsk.Close()

	opf := etree.NewDocument()
	_, err = opf.ReadFrom(rrsk)
	if err != nil {
		return "", 0, err
	}

	var series string
	for _, e := range opf.FindElements("//meta[@name='calibre:series']") {
		series = e.SelectAttrValue("content", "")
		break
	}

	var seriesNumber float64
	for _, e := range opf.FindElements("//meta[@name='calibre:series_index']") {
		i, err := strconv.ParseFloat(e.SelectAttrValue("content", "0"), 64)
		if err == nil {
			seriesNumber = i
			break
		}
	}

	return series, seriesNumber, nil
}

func main() {
	help := pflag.BoolP("help", "h", false, "Show this help message")
	pflag.Parse()

	if *help || pflag.NArg() > 1 {
		helpExit()
	}

	log := func(format string, a ...interface{}) {
		fmt.Printf(format, a...)
	}

	logE := func(format string, a ...interface{}) {
		fmt.Fprintf(os.Stderr, format, a...)
	}

	var kpath string
	if pflag.NArg() == 1 {
		kpath = strings.Replace(pflag.Arg(0), ".kobo", "", 1)
	} else {
		log("No kobo specified, attempting to detect one\n")
		kobos, err := kobo.Find()
		if err != nil {
			logE("Fatal: could not automatically detect a kobo: %v\n", err)
			errExit()
		} else if len(kobos) < 1 {
			logE("Fatal: could not automatically detect a kobo\n")
			errExit()
		}
		kpath = kobos[0]
	}

	log("Checking kobo at '%s'\n", kpath)
	if !kobo.IsKobo(kpath) {
		logE("Fatal: '%s' is not a valid kobo\n", kpath)
	}

	kpath, err := filepath.Abs(kpath)
	if err != nil {
		logE("Fatal: Could not resolve path to kobo\n")
		errExit()
	}

	dbpath := filepath.Join(kpath, ".kobo", "KoboReader.sqlite")

	log("Making backup of KoboReader.sqlite\n")
	err = copyFile(dbpath, dbpath+".bak")
	if err != nil {
		logE("Fatal: Could not make copy of KoboReader.sqlite: %v\n", err)
		errExit()
	}

	log("Opening KoboReader.sqlite\n")
	db, err := sql.Open("sqlite3", dbpath)
	if err != nil {
		logE("Fatal: Could not open KoboReader.sqlite: %v\n", err)
		errExit()
	}

	log("Searching for sideloaded epubs and kepubs\n")
	epubs, err := zglob.Glob(filepath.Join(kpath, "**", "*.epub"))
	if err != nil {
		logE("Fatal: Could not search for epubs: %v\n", err)
		errExit()
	}

	log("\nUpdating metadata for %d books\n", len(epubs))
	var updated, nometa, errcount int
	digits := len(fmt.Sprint(len(epubs)))
	numFmt, spFmt := fmt.Sprintf("[%%%dd/%d] ", digits, len(epubs)), strings.Repeat(" ", (digits*2)+4)
	for i, epub := range epubs {
		rpath, err := filepath.Rel(kpath, epub)
		if err != nil {
			log(numFmt+"%s\n", i+1, epub)
			logE(spFmt+"Error: could not resolve path: %v\n", err)
			errcount++
			continue
		}

		log(numFmt+"%s\n", i+1, rpath)
		series, seriesNumber, err := getMeta(epub)
		if err != nil {
			logE(spFmt+"Error: could not read metadata: %v\n", err)
			errcount++
			continue
		}

		if series == "" && seriesNumber == 0 {
			nometa++
			continue
		}

		log(spFmt+"(%s, %v)\n", series, seriesNumber)

		iid := contentIDToImageID(pathToContentID(rpath))

		res, err := db.Exec("UPDATE content SET Series=?, SeriesNumber=? WHERE ImageID=?", sql.NullString{
			String: series,
			Valid:  series != "",
		}, sql.NullString{
			String: fmt.Sprintf("%v", seriesNumber),
			Valid:  seriesNumber > 0,
		}, iid)
		if err != nil {
			logE(spFmt+"Error: could not update database: %v\n", err)
			errcount++
			continue
		}

		ra, err := res.RowsAffected()
		if err != nil {
			logE(spFmt+"Error: could not update database: %v\n", err)
			errcount++
			continue
		}

		if ra > 1 {
			logE(spFmt + "Warn: more than one match in database for ImageID\n")
		} else if ra < 1 {
			logE(spFmt + "Error: could not update database: no entry in database for book (the kobo may still need to import the book)\n")
			errcount++
			continue
		}

		updated++
	}

	time.Sleep(time.Second)
	log("\nFinished updating metadata. %d updated, %d without metadata, %d errored.\n", updated, nometa, errcount)

	if runtime.GOOS == "windows" {
		time.Sleep(time.Second * 2)
	}
}
