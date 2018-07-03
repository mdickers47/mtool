package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/mdickers47/mtool/db"
)

type Transformer struct {
	Image func([]db.MasterFile) []db.ImageFile
	Make  func(db.ImageFile) error
}

var xfm_byname = map[string]Transformer{
	"opus": Transformer{db.ImageOpus, db.MakeOpus},
	"webm": Transformer{db.ImageWebm, db.MakeWebm},
}

var Parallelism = flag.Int("j", 1, "how many make threads to run in parallel")

func MakeImage(mdb *db.MediaDB, which string, root string) error {

	xfm, ok := xfm_byname[which]
	if !ok {
		return fmt.Errorf("invalid transform type: %v", which)
	}

	if len(which) == 0 {
		return fmt.Errorf("output path must be specified")
	}
	which, err := db.ExpandTilde(which)
	if err != nil {
		return err
	}

	imfs := xfm.Image(mdb.MasterFiles)
	fmt.Printf("master files: %v image files: %v\n",
		len(mdb.MasterFiles), len(imfs))

	var keep_imfs []db.ImageFile

	for _, imf := range imfs {
		imf.ImagePath = filepath.Join(root, imf.ImagePath)
		stat, err := os.Stat(imf.ImagePath)
		if err != nil {
			keep_imfs = append(keep_imfs, imf)
		} else {
			imf.ImageMtime = stat.ModTime()
			if imf.ImageMtime.Unix() < imf.MasterMtime.Unix() {
				keep_imfs = append(keep_imfs, imf)
			}
		}
	}

	fmt.Printf("%v image files present, %v to make\n",
		len(imfs)-len(keep_imfs), len(keep_imfs))

	imfchan := make(chan db.ImageFile)
	var wg sync.WaitGroup
	wg.Add(*Parallelism)

	for i := 0; i < *Parallelism; i++ {
		go func() {
			defer wg.Done()
			for imf := range imfchan {
				if err := xfm.Make(imf); err != nil {
					fmt.Printf("error: %v\n", err)
				}
			}
		}()
	}
	for _, imf := range keep_imfs {
		imfchan <- imf
	}
	close(imfchan)
	wg.Wait()

	return nil
}

func main() {
	flag.Parse()

	mediadb, err := db.LoadDb()
	if err != nil {
		fmt.Printf("unable to load %v: %v\n", *db.Dbfile, err)
		fmt.Print("fix the problem or re-initialize with 'init /path/to/root'\n")
	}
	fmt.Printf("library contains %v files\n", len(mediadb.MasterFiles))

	dirtydb := false

	switch {
	case flag.Arg(0) == "scan" && flag.NArg() == 1:
		fmt.Printf("rescanning %v\n", mediadb.FileRoot)
		mediadb, err = db.ScanFiles(mediadb, os.Stdout)
		dirtydb = true
	case flag.Arg(0) == "init" && flag.NArg() == 2:
		fmt.Printf("creating new library from %v\n", flag.Arg(1))
		mediadb = db.MediaDB{flag.Arg(1), make([]db.MasterFile, 0, 100)}
		mediadb, err = db.ScanFiles(mediadb, os.Stdout)
		dirtydb = true
	case flag.Arg(0) == "find" && flag.NArg() > 1:
		var paths []string
		for i := 1; i < flag.NArg(); i++ {
			new_paths, err := mediadb.RegexSearch(flag.Arg(i))
			if err != nil {
				fmt.Printf("regex error: %v\n", err)
				return
			}
			paths = append(paths, new_paths...)
		}
		for i := range paths {
			fmt.Println(paths[i])
		}
	case flag.Arg(0) == "latest" && flag.NArg() <= 2:
		var n int
		if flag.NArg() == 1 {
			n = 10
		} else {
			n, err = strconv.Atoi(flag.Arg(1))
			if err != nil {
				fmt.Printf("bad argument: %v\n", err)
				return
			}
		}
		paths := mediadb.Latest(n)
		for i := range paths {
			fmt.Println(paths[i])
		}
	case flag.Arg(0) == "make" && flag.NArg() == 3:
		err = MakeImage(&mediadb, flag.Arg(1), flag.Arg(2))
		if err != nil {
			fmt.Println(err)
			return
		}
	default:
		flag.Usage()
		return
	}

	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return
	}

	if dirtydb {
		fmt.Printf("saving library %v of %v files\n",
			*db.Dbfile, len(mediadb.MasterFiles))
		err = db.SaveDb(mediadb)
		if err != nil {
			fmt.Printf("failed to save: %v\n", err)
		}
	}

}
