package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/geek1011/kepubify/kepub"
	isatty "github.com/mattn/go-isatty"
	zglob "github.com/mattn/go-zglob"
	"github.com/spf13/pflag"
)

var version = "dev"

func helpExit() {
	fmt.Fprintf(os.Stderr, "Usage: kepubify [OPTIONS] PATH [PATH]...\n\nVersion:\n  kepubify %s\n\nOptions:\n", version)
	pflag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nArguments:\n  PATH is the path to an epub file or directory to convert. If it is a directory, the converted dir is the name of the dir with the suffix _converted. If the path is a file, the converted file has the extension .kepub.epub.\n")
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

func main() {
	help := pflag.BoolP("help", "h", false, "show this help text")
	sversion := pflag.Bool("version", false, "show the version")
	update := pflag.BoolP("update", "u", false, "don't reconvert files which have already been converted")
	verbose := pflag.BoolP("verbose", "v", false, "show extra information in output")
	output := pflag.StringP("output", "o", ".", "the directory to place the converted files")
	css := pflag.StringP("css", "c", "", "custom CSS to add to ebook")
	hyphenate := pflag.Bool("hyphenate", false, "force enable hyphenation")
	nohyphenate := pflag.Bool("no-hyphenate", false, "force disable hyphenation")
	inlinestyles := pflag.Bool("inline-styles", false, "inline all stylesheets (for working around certain bugs)")
	fullscreenfixes := pflag.Bool("fullscreen-reading-fixes", false, "enable fullscreen reading bugfixes based on https://www.mobileread.com/forums/showpost.php?p=3113460&postcount=16")
	replace := pflag.StringArrayP("replace", "r", nil, "find and replace on all html files (repeat any number of times) (format: find|replace)")
	pflag.Parse()

	if *sversion {
		fmt.Printf("kepubify %s\n", version)
		os.Exit(0)
	}

	if *help || pflag.NArg() == 0 {
		helpExit()
	}

	if *hyphenate && *nohyphenate {
		fmt.Printf("--hyphenate and --no-hyphenate are mutally exclusive\n")
		helpExit()
	}

	logV := func(format string, a ...interface{}) {
		if *verbose {
			if os.Getenv("TERM") != "dumb" && (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && isatty.IsTerminal(os.Stdout.Fd()) {
				fmt.Print("\033[36m")
				fmt.Printf(format, a...)
				fmt.Print("\033[0m")
				return
			}
			fmt.Printf(format, a...)
		}
	}

	log := func(format string, a ...interface{}) {
		fmt.Printf(format, a...)
	}

	logE := func(format string, a ...interface{}) {
		fmt.Fprintf(os.Stderr, format, a...)
	}

	out := ""
	out, err := filepath.Abs(*output)
	if err != nil || out == "" {
		logE("Error resolving output dir '%s': %v\n", *output, err)
		errExit()
	}

	logV("version: %s\n\n", version)
	logV("output: %s\n", *output)
	logV("output-abs: %s\n", out)
	logV("help: %t\n", *help)
	logV("update: %t\n", *update)
	logV("verbose: %t\n", *verbose)
	logV("css: %s\n", *css)
	logV("hyphenate: %t\n", *hyphenate)
	logV("nohyphenate: %t\n", *nohyphenate)
	logV("inlinestyles: %t\n\n", *inlinestyles)
	logV("fullscreenfixes: %t\n\n", *fullscreenfixes)
	logV("replace: %s\n\n", strings.Join(*replace, ","))

	findReplace := map[string]string{}
	for _, r := range *replace {
		spl := strings.SplitN(r, "|", 2)
		if len(spl) != 2 {
			logE("Error parsing replacement '%s': must be in format `find|replace`\n", r)
			errExit()
		}
		findReplace[spl[0]] = spl[1]
	}

	converter := &kepub.Converter{
		ExtraCSS:        *css,
		Hyphenate:       *hyphenate,
		NoHyphenate:     *nohyphenate,
		InlineStyles:    *inlinestyles,
		FullScreenFixes: *fullscreenfixes,
		FindReplace:     findReplace,
		Verbose:         *verbose,
	}

	paths := map[string]string{}
	for _, arg := range uniq(pflag.Args()) {
		if !exists(arg) {
			logE("Path '%s' does not exist\n", arg)
			errExit()
		}
		if isFile(arg) {
			logV("file: %s\n", arg)
			f, err := filepath.Abs(arg)
			if err != nil {
				logE("Error resolving absolute path for file '%s'\n", arg)
				errExit()
			}
			if !strings.HasSuffix(f, ".epub") {
				logE("File '%s' is not an epub\n", f)
				errExit()
			}
			if strings.HasSuffix(f, ".kepub.epub") {
				logE("File '%s' is already a kepub\n", f)
				errExit()
			}
			paths[f] = filepath.Join(out, strings.Replace(filepath.Base(f), ".epub", "", -1)+".kepub.epub")
			logV("  file-result: %s -> %s\n", f, paths[f])
		} else if isDir(arg) {
			argabs, err := filepath.Abs(arg)
			if err != nil {
				logE("Error resolving path for dir '%s'\n", arg)
				errExit()
			}
			logV("dir: %s\n", arg)
			l, err := zglob.Glob(filepath.Join(arg, "**", "*.epub"))
			if err != nil {
				logV("Error scanning dir '%s'\n", arg)
				errExit()
			}
			for _, f := range l {
				logV("  dir-file: %s\n", f)
				if !strings.HasSuffix(f, ".epub") || strings.HasSuffix(f, ".kepub.epub") {
					continue
				}

				rel, err := filepath.Rel(arg, filepath.Join(filepath.Dir(f), strings.Replace(filepath.Base(f), ".epub", "", -1)+".kepub.epub"))
				if err != nil {
					logE("Error resolving relative path for file '%s'\n", f)
					errExit()
				}

				abs, err := filepath.Abs(f)
				if err != nil {
					logE("Error resolving absolute path for file '%s'\n", f)
					errExit()
				}

				paths[abs] = filepath.Join(out, filepath.Base(argabs)+"_converted", rel)
				logV("    dir-result: %s -> %s\n", abs, paths[abs])
			}
		} else {
			logE("Path '%s' is not a file or a dir\n", arg)
			errExit()
		}
	}

	logV("\n")

	log("Kepubify %s: Converting %d books\n", version, len(paths))
	log("Output folder: %s\n", out)

	n := 0
	errs := [][]string{}
	converted := 0
	skipped := 0
	errored := 0
	for i, o := range paths {
		n++
		e := exists(o)
		if e && *update {
			log("[%d/%d] Skipping '%s'\n", n, len(paths), i)
		} else {
			log("[%d/%d] Converting '%s'\n", n, len(paths), i)
		}

		de := isDir(filepath.Dir(o))

		logV("  i: %s\n", i)
		logV("  o: %s\n", o)
		logV("  e: %t\n", e)
		logV("  de: %t\n", de)

		if e && *update {
			skipped++
			continue
		}

		if !de {
			logV("  mkdirAll: %s\n", filepath.Dir(o))
			err := os.MkdirAll(o, os.ModePerm)
			if err != nil {
				e := fmt.Sprintf("error creating output dir: %v", err)
				errs = append(errs, []string{i, o, e})
				logV("  err: %v\n", e)
				logE("  Error: %v\n", e)
				errored++
				continue
			}
		}

		err := converter.Convert(i, o)
		if err != nil {
			errs = append(errs, []string{i, o, err.Error()})
			logV("  err: %v\n", err)
			logE("  Error: %v\n", err)
			errored++
		}

		converted++
	}

	logV("\nn: %d\n", n)
	logV("converted: %d\n", converted)
	logV("skipped: %d\n", skipped)
	logV("errored: %d\n", errored)
	logV("errs: %v\n", errs)

	log("\n%d total, %d converted, %d skipped, %d errored\n", len(paths), converted, skipped, errored)
	if len(errs) > 0 {
		logE("\nErrors:\n")
		for _, err := range errs {
			logE("  '%s': %s\n", err[0], err[2])
		}
	}

	if len(paths) == 1 && len(errs) > 0 {
		errExit()
	}
}
