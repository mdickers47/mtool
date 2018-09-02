package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mdickers47/mtool/db"
	"github.com/mdickers47/mtool/xfm"
)

type Command struct {
	Func    func(mdb *db.MediaDB, args []string) bool
	Help    string
	MinArgs int
	MaxArgs int
}

var CommandByName = map[string]Command{
	"info": {Info,
		"show information about library and available imagers", 0, 0},
	"scan": {Scan,
		"re-scan filesystem and update library", 0, 0},
	"init": {Init,
		"initialize library (arg: /path/to/root)", 1, 1},
	"find": {Find,
		"regex search metadata and output master files (argv: regexes)", 1, 1024},
	"latest": {Latest,
		"output most recent n master files (arg: n)", 1, 1},
	"make": {Make,
		"transcode and create output image (args: imager, /output/path)", 2, 2},
}

func Info(mdb *db.MediaDB, args []string) bool {
	fmt.Printf("library file at %v contains %v master files\n", *db.Dbfile,
		len(mdb.MasterFiles))
	i, keys := 0, make([]string, len(db.HandlerByExt))
	for key, _ := range db.HandlerByExt {
		keys[i] = key
		i++
	}
	fmt.Printf("available master file handlers: %v\n", strings.Join(keys, ", "))
	i, keys = 0, make([]string, len(xfm.Byname))
	for key, _ := range xfm.Byname {
		keys[i] = key
		i++
	}
	fmt.Printf("available image types: %v\n", strings.Join(keys, ", "))
	return false
}

func Scan(mdb *db.MediaDB, _ []string) bool {
	fmt.Printf("rescanning %v\n", mdb.FileRoot)
	err := db.ScanFiles(mdb, os.Stdout)
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	return true
}

func Init(mdb *db.MediaDB, args []string) bool {
	fmt.Printf("creating new library from %v\n", args[0])
	mdb.FileRoot = args[0]
	mdb.MasterFiles = make([]db.MasterFile, 0, 100)
	err := db.ScanFiles(mdb, os.Stdout)
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	return true
}

func Find(mdb *db.MediaDB, args []string) bool {
	var paths []string
	for _, arg := range args {
		new_paths, err := mdb.RegexSearch(arg)
		if err != nil {
			fmt.Printf("regex error: %v\n", err)
			return false
		}
		paths = append(paths, new_paths...)
	}
	for i := range paths {
		fmt.Println(paths[i])
	}
	return false
}

func Latest(mdb *db.MediaDB, args []string) bool {
	var n int
	if len(args) == 0 {
		n = 10
	} else {
		var err error
		n, err = strconv.Atoi(args[1])
		if err != nil {
			fmt.Printf("bad argument: %v\n", err)
			return false
		}
	}
	paths := mdb.Latest(n)
	for i := range paths {
		fmt.Println(paths[i])
	}
	return false
}

func Make(mdb *db.MediaDB, args []string) bool {
	err := xfm.MakeImage(mdb, args[0], args[1])
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	return false
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: mtool [-flags] command arg...\n\n")
	fmt.Fprintf(os.Stderr, "flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\ncommands:\n")
	for key, val := range CommandByName {
		fmt.Fprintf(os.Stderr, "  %-8v %v\n", key, val.Help)
	}
}

func main() {
	flag.Parse()

	mediadb, err := db.LoadDb()
	if err != nil {
		fmt.Printf("unable to load %v: %v\n", *db.Dbfile, err)
		fmt.Print("fix the problem or re-initialize with 'init /path/to/root'\n")
	}

	cmd, ok := CommandByName[flag.Arg(0)]
	if !ok {
		usage()
		return
	}

	args := flag.Args()[1:]
	if len(args) < cmd.MinArgs || len(args) > cmd.MaxArgs {
		usage()
		return
	}

	dirtydb := cmd.Func(&mediadb, args)

	if dirtydb {
		fmt.Printf("saving library %v of %v files\n",
			*db.Dbfile, len(mediadb.MasterFiles))
		err = db.SaveDb(mediadb)
		if err != nil {
			fmt.Printf("failed to save: %v\n", err)
		}
	}

}
