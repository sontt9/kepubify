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
	"github.com/ogier/pflag"
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
	help := pflag.BoolP("help", "h", false, "Show this help text")
	update := pflag.BoolP("update", "u", false, "Don't reconvert files which have already been converted")
	verbose := pflag.BoolP("verbose", "v", false, "Show extra information in output")
	output := pflag.StringP("output", "o", ".", "The directory to place the converted files")
	pflag.Parse()

	if *help || pflag.NArg() == 0 {
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
	logV("verbose: %t\n\n", *verbose)

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

				paths[abs] = filepath.Join(out, rel)
				logV("    dir-result: %s -> %s\n", abs, paths[abs])
			}
		} else {
			logE("Path '%s' is not a file or a dir\n", arg)
			errExit()
		}
	}

	logV("\n")

	log("Kepubify %s: Converting %d books\n", version, len(paths))

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

		err := kepub.Kepubify(i, o, *verbose)
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
