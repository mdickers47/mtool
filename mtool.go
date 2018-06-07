package main

import (
	"flag"
	"fmt"
	"os"
	"github.com/mdickers47/mtool/db"
)


func main() {
	flag.Parse()

	mediadb, err := db.LoadDb()
	if err != nil {
		fmt.Printf("unable to load %v: %v\n", *db.Dbfile, err)
		fmt.Print("fix the problem or re-initialize with 'init /path/to/root'\n")
	}
	fmt.Printf("library contains %v files\n", len(mediadb.MediaFiles))

	dirtydb := false

	switch {
	case flag.Arg(0) == "scan" && flag.NArg() == 1:
		fmt.Printf("rescanning %v\n", mediadb.FileRoot)
		mediadb, err = db.ScanFiles(mediadb, os.Stdout)
		dirtydb = true
	case flag.Arg(0) == "init" && flag.NArg() == 2:
		fmt.Printf("creating new library from %v\n", flag.Arg(1))
		mediadb = db.MediaDB{flag.Arg(1), make([]db.MediaFile, 100)}
		mediadb, err = db.ScanFiles(mediadb, os.Stdout)
		dirtydb = true
	default:
		flag.Usage()
		return
	}

	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return
	}

	if dirtydb {
  	fmt.Printf("saving library %v of %v files\n", *db.Dbfile, len(mediadb.MediaFiles))
	  err = db.SaveDb(mediadb)
	  if err != nil {
		  fmt.Printf("failed to save: %v\n", err)
		}
	}
	
}
