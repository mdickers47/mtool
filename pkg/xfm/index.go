package xfm

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mdickers47/mtool/pkg/db"
)

var Parallelism = flag.Int("j", 1, "how many make threads to run in parallel")

type Transformer struct {
	Image func([]db.MasterFile) []db.ImageFile
	Make  func(db.ImageFile) error
}

var Byname = map[string]Transformer{
	"opus": Transformer{ImageOpus, MakeOpus},
	"webm": Transformer{ImageWebm, MakeWebm},
	"aac":  Transformer{ImageAac, MakeAac},
}

func MakeImage(mdb *db.MediaDB, which string, root string) error {

	xfmr, ok := Byname[which]
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

	imfs := xfmr.Image(mdb.MasterFiles)
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
				if err := xfmr.Make(imf); err != nil {
					fmt.Printf("%v: %v\n", imf.ImagePath, err)
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
