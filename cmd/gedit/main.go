package main

import (
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli"
)

var (
	version = "v0.0.0"
)

type FileReader interface {
	Open(string) (io.ReadCloser, error)
}

type LocalFileReader struct{}

func (lfr *LocalFileReader) Open(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

type URLFileReader struct {
	httpClient http.Client
}

func (ufr *URLFileReader) Open(path string) (io.ReadCloser, error) {
	resp, err := ufr.httpClient.Get(path)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

var (
	outputDirFlagValue string
	outputDirFlag      = cli.StringFlag{
		Name:        "output-dir",
		Value:       "",
		Destination: &outputDirFlagValue,
	}
	outputFileFlagValue string
	outputFileFlag      = cli.StringFlag{
		Name:        "output-file",
		Value:       "",
		Destination: &outputFileFlagValue,
	}
)

func main() {
	logger := log.New(os.Stdout, "[gedit] ", log.LstdFlags)
	logger.Println("Starting...")

	app := cli.NewApp()
	app.Name = "gedit"
	app.Version = version
	app.Action = cli.ShowAppHelp

	app.Commands = []cli.Command{
		cli.Command{
			Name:        "unpack",
			Action:      func(c *cli.Context) error { return unpack(c, logger) },
			Description: "unpack a gif into its images",
			Flags:       []cli.Flag{outputDirFlag},
		},
		cli.Command{
			Name:        "pack",
			Action:      func(c *cli.Context) error { return pack(c, logger) },
			Description: "pack a set of images into a gif",
			Flags:       []cli.Flag{outputFileFlag},
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatalln("App exiting with error:", err.Error())
	}
	os.Exit(0)
}

func pack(ctx *cli.Context, logger *log.Logger) error {
	if ctx.NArg() < 1 {
		return errors.New("expected filename to be first argument")
	}
	path := ctx.Args()[0]

	if stat, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			logger.Printf("Directory does not exist: '%s'.\n", path)
			return err
		}
		return err
	} else if !stat.IsDir() {
		logger.Printf("File '%s' is not a directory.\n", path)
		return err
	}

	logger.Printf("Opening directory '%s'.\n", path)
	dir, err := os.Open(path)
	if err != nil {
		logger.Println("Failed opening file:", err.Error())
		return err
	}
	defer dir.Close()

	logger.Println("Reading directory...")
	infos, err := dir.Readdir(-1)
	if err != nil {
		logger.Println("Failed reading file info from directory:", err.Error())
		return err
	}

	start := time.Now()
	filePaths := make([]string, 0)
	for _, info := range infos {
		if info.IsDir() {
			logger.Printf("Skipping directory '%s'.\n", info.Name())
			continue
		}
		logger.Printf("Discoverd file '%s'.\n", info.Name())
		filePaths = append(filePaths, info.Name())
	}
	logger.Printf("Discovered %d files in %dms.\n", len(filePaths), time.Since(start).Nanoseconds()/1000000)

	logger.Println("Packing images...")
	start = time.Now()
	newGif := &gif.GIF{}
	for _, filePath := range filePaths {
		file, err := os.Open(filepath.Join(path, filePath))
		if err != nil {
			logger.Println("Error opening file:", err.Error())
			return err
		}
		defer file.Close()
		asGif, err := png.Decode(file)
		if err != nil {
			logger.Println("Failed decoding file as gif:", err.Error())
			return err
		}
		newGif.Image = append(newGif.Image, asGif.(*image.Paletted))
		newGif.Delay = append(newGif.Delay, 0)
	}
	logger.Printf("Finished packing in %dms.\n", time.Since(start).Nanoseconds()/1000000)

	logger.Println("Creating gif...")
	outputFilePath := outputFileFlagValue
	outputFile, err := os.OpenFile(outputFilePath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		logger.Println("Failed opening output file:", err.Error())
		return err
	}
	defer outputFile.Close()
	if err := gif.EncodeAll(outputFile, newGif); err != nil {
		logger.Println("Failed encoding gif:", err.Error())
		return err
	}

	logger.Printf("Created '%s'\n", outputFilePath)

	return nil
}

func unpack(ctx *cli.Context, logger *log.Logger) error {
	if ctx.NArg() < 1 {
		return errors.New("expected filename to be first argument")
	}
	path := ctx.Args()[0]
	// Resolve the correct file reader based off the path
	local := false
	var fileReader FileReader
	if strings.HasPrefix(path, "http") {
		fileReader = &URLFileReader{http.Client{Timeout: time.Second * 10}}
		logger.Println("Using url reader.")
	} else {
		fileReader = &LocalFileReader{}
		logger.Println("Using local file reader.")
		local = true
	}

	// Open the file
	logger.Printf("Attempting to open '%s'\n", path)
	start := time.Now()
	reader, err := fileReader.Open(path)
	if err != nil {
		logger.Println("Failed opening file:", err.Error())
		return err
	}
	defer reader.Close()
	logger.Printf("Successfully opened the file in %dms.\n", time.Since(start).Nanoseconds()/1000000)

	// Decode the gif
	logger.Println("Attempting to decode gif.")
	start = time.Now()
	g, err := gif.DecodeAll(reader)
	if err != nil {
		logger.Println("Failed decoding gif:", err.Error())
		return err
	}
	logger.Printf("Successfully decoded the gif in %dms.\n", time.Since(start).Nanoseconds()/1000000)
	logger.Println("Gif Properties:")
	logger.Printf("Dimensions: %dx%d\n", g.Config.Width, g.Config.Height)
	logger.Printf("Frames: %d\n", len(g.Image))

	outputDir := ctx.String(outputDirFlag.Name)
	if outputDir == "" {
		if local {
			outputDir = filepath.Dir(path)
			logger.Printf("Using local directory based off input: '%s'.\n", outputDir)
		} else {
			outputDir = "output"
			logger.Printf("Defaulting to '%s'.\n", outputDir)
		}
	} else {
		logger.Printf("Using configured output directory: '%s'.\n", outputDir)
	}

	// If directory doesn't exist, create it.
	if _, err := os.Stat(outputDir); err != nil {
		if os.IsNotExist(err) {
			logger.Println("Attempting to create output directory...")
			if err := os.MkdirAll(outputDir, os.ModeDir); err != nil {
				logger.Printf("Failed creating directory: '%s'.\n", outputDir)
				return err
			}
			logger.Println("Created output directory.")
		}
	} else {
		logger.Println("Output Directory exists.")
	}

	start = time.Now()
	logger.Println("Unpacking images.")
	baseFilePath := filepath.Join(outputDir, strings.Replace(filepath.Base(path), filepath.Ext(path), "", -1))
	for index, i := range g.Image {
		fileName := fmt.Sprintf("%s_%d.png", baseFilePath, index)
		logger.Printf("Creating file for image %d: '%s'\n", index, fileName)
		file, err := os.Create(fileName)
		if err != nil {
			return err
		}
		if err := png.Encode(file, i); err != nil {
			logger.Println("Failed writing file:", err.Error())
		}
		file.Close()
	}
	logger.Printf("Finished unpacking in %dms.\n", time.Since(start).Nanoseconds()/1000000)

	return nil
}
