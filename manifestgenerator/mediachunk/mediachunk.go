package mediachunk

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"

	"github.com/sirupsen/logrus"
)

// OutputTypes indicates the manifest type
type OutputTypes int

const (
	// OutputModeNone No no write data
	OutputModeNone OutputTypes = iota

	// OutputModeFile Saves chunks to file
	OutputModeFile

	// OutputModeHTTP chunks to chunked streaming server
	OutputModeHTTP
)

// Options Chunking options
type Options struct {
	Log                *logrus.Logger
	OutputType         OutputTypes
	LHLS               bool
	EstimatedDurationS float64
	FileNumberLength   int
	GhostPrefix        string
	FileExtension      string
	BasePath           string
	ChunkBaseFilename  string
	HTTPClient         *http.Client
	HTTPScheme         string
	HTTPHost           string
}

// Chunk Chunk class
type Chunk struct {
	fileWriter     *bufio.Writer
	fileDescriptor *os.File

	httpWriteChan chan<- []byte
	httpReq       *http.Request

	options Options

	index         uint64
	filename      string
	filenameGhost string
}

// New Creates a chunk instance
func New(index uint64, options Options) Chunk {
	c := Chunk{nil, nil, nil, nil, options, index, "", ""}

	c.filename = c.createFilename(options.BasePath, options.ChunkBaseFilename, index, options.FileNumberLength, options.FileExtension, "")
	if options.GhostPrefix != "" {
		c.filenameGhost = c.createFilename(options.BasePath, options.ChunkBaseFilename, index, options.FileNumberLength, options.FileExtension, options.GhostPrefix)
	}

	return c
}

//InitializeChunk Initializes chunk
func (c *Chunk) InitializeChunk() error {
	if c.options.OutputType == OutputModeFile {
		if c.filenameGhost != "" {
			// Create ghost file
			exists, _ := fileExists(c.filenameGhost)
			if !exists {
				err := ioutil.WriteFile(c.filenameGhost, nil, 0644)
				if err != nil {
					return err
				}
			}
		}

		if c.filename != "" {
			// Create ghost file
			exists, _ := fileExists(c.filename)
			if !exists {
				var err error
				c.fileDescriptor, err = os.Create(c.filename)
				if err != nil {
					return err
				}

				c.fileWriter = bufio.NewWriter(c.fileDescriptor)
			}
		}
	}

	if c.options.OutputType == OutputModeHTTP {
		r, w := io.Pipe()
		writeChan := make(chan []byte)
		c.httpWriteChan = writeChan

		// open request
		req := &http.Request{
			Method: "POST",
			URL: &url.URL{
				Scheme: c.options.HTTPScheme,
				Host:   c.options.HTTPHost,
				Path:   "/" + c.filename,
			},
			ProtoMajor:    1,
			ProtoMinor:    1,
			ContentLength: -1,
			Body:          r,
		}

		c.httpReq = req

		go func() {
			defer w.Close()

			for buf := range writeChan {
				n, err := w.Write(buf)
				c.options.Log.Debug("Wrote ", n, " bytes to chunk ", c.filename)
				if n != len(buf) && err != nil {
					panic(err)
				}
			}
		}()

		go func() {
			c.options.Log.Debug("Opening connection to upload ", c.filename)
			c.options.Log.Debug("Req: ", req)
			_, err := c.options.HTTPClient.Do(req)

			if err != nil {
				c.options.Log.Error("Error uploading ", c.filename, ". Error: ", err)
			} else {
				c.options.Log.Debug("Upload of ", c.filename, " complete")
			}
		}()
	}

	return nil
}

//Close Closes chunk
func (c *Chunk) Close() {
	c.options.Log.Debug("Closing chunk ", c.filename)
	if c.options.OutputType == OutputModeFile {
		if c.filenameGhost != "" {
			exists, _ := fileExists(c.filenameGhost)
			if exists {
				os.Remove(c.filenameGhost)
			}
		}

		if c.fileWriter != nil {
			c.fileDescriptor.Close()
		}
	}

	if c.options.OutputType == OutputModeHTTP {
		if c.httpWriteChan != nil {
			close(c.httpWriteChan)
		}
	}

	return
}

//AddData Add data to chunk and flush it
func (c *Chunk) AddData(buf []byte) error {
	c.options.Log.Debug("Adding data to chunk ", c.filename)
	if c.options.OutputType == OutputModeFile {
		if c.fileWriter != nil {
			totalWrittenBytes := 0
			err := error(nil)

			for totalWrittenBytes < len(buf) && err == nil {
				writtenBytes, err := c.fileWriter.Write(buf[totalWrittenBytes:])

				totalWrittenBytes = totalWrittenBytes + writtenBytes

				if err != nil {
					return err
				}
			}
			c.fileWriter.Flush()
		}
	} else if c.options.OutputType == OutputModeHTTP {
		if c.httpWriteChan != nil {
			c.httpWriteChan <- buf
			c.fileWriter.Flush()
		}
	}

	return nil
}

//GetFilename Add data to chunk and flush it
func (c *Chunk) GetFilename() string {
	return c.filename
}

func fileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

func padNumberWithZero(value uint64, numZeros int) string {
	format := "%0" + strconv.Itoa(numZeros) + "d"
	return fmt.Sprintf(format, value)
}

func (c *Chunk) createFilename(
	basePath string,
	chunkBaseFilename string,
	index uint64,
	fileNumberLength int,
	fileExtension string,
	ghostPrefix string,
) string {
	ret := ""
	if ghostPrefix != "" {
		ret = path.Join(basePath, ghostPrefix+chunkBaseFilename+padNumberWithZero(index, fileNumberLength)+fileExtension)
	} else {
		ret = path.Join(basePath, chunkBaseFilename+padNumberWithZero(index, fileNumberLength)+fileExtension)
	}

	return ret
}
