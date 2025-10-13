// main.go
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
	"howett.net/plist"
)

// DSStore holds the entire parsed structure of the .DS_Store file.
type DSStore struct {
	content []byte
	reader  *bytes.Reader
	records map[string]map[string]interface{}
}

// NewDSStore creates and parses a DS_Store structure from a byte slice.
func NewDSStore(content []byte) (*DSStore, error) {
	d := &DSStore{
		content: content,
		reader:  bytes.NewReader(content),
		records: make(map[string]map[string]interface{}),
	}
	if err := d.parse(); err != nil {
		return nil, err
	}
	return d, nil
}

// readUint32 reads a 4-byte big-endian unsigned integer.
func (d *DSStore) readUint32() (uint32, error) {
	var val uint32
	err := binary.Read(d.reader, binary.BigEndian, &val)
	return val, err
}

// readUint64 reads an 8-byte big-endian unsigned integer.
func (d *DSStore) readUint64() (uint64, error) {
	var val uint64
	err := binary.Read(d.reader, binary.BigEndian, &val)
	return val, err
}

// parse is the main parsing entrypoint.
func (d *DSStore) parse() error {
	d.reader.Seek(4, io.SeekStart) // Skip alignment bytes
	magic, err := d.readUint32()
	if err != nil {
		return err
	}
	if magic != 0x42756431 { // 'Bud1'
		fmt.Fprintln(os.Stderr, "Warning: File magic number is not 'Bud1'. This may not be a valid .DS_Store file.")
	}

	allocatorOffset, err := d.readUint32()
	if err != nil {
		return err
	}

	tocOffset := int64(allocatorOffset) + 4 + 1032 // 0x408 offset
	d.reader.Seek(tocOffset, io.SeekStart)
	numTocEntries, err := d.readUint32()
	if err != nil {
		return err
	}

	var masterID uint32 = 0
	for i := 0; i < int(numTocEntries); i++ {
		keyLenByte, err := d.reader.ReadByte()
		if err != nil {
			return err
		}
		key := make([]byte, keyLenByte)
		if _, err := d.reader.Read(key); err != nil {
			return err
		}
		val, err := d.readUint32()
		if err != nil {
			return err
		}
		if string(key) == "DSDB" {
			masterID = val
			break
		}
	}
	if masterID == 0 {
		return fmt.Errorf("could not find 'DSDB' master block in the allocator")
	}

	masterBlockOffset, _, err := d.getBlockInfo(allocatorOffset, masterID)
	if err != nil {
		return err
	}
	d.reader.Seek(masterBlockOffset, io.SeekStart)
	rootNodeID, err := d.readUint32()
	if err != nil {
		return err
	}

	return d.parseNode(allocatorOffset, rootNodeID)
}

// getBlockInfo calculates the offset and size of a data block.
func (d *DSStore) getBlockInfo(allocatorOffset, blockID uint32) (offset int64, size uint32, err error) {
	offsetsTableStart := int64(allocatorOffset) + 4 + 8
	blockInfoOffset := offsetsTableStart + int64(blockID)*4

	currentPos, _ := d.reader.Seek(0, io.SeekCurrent)
	defer d.reader.Seek(currentPos, io.SeekStart) // Restore reader position

	d.reader.Seek(blockInfoOffset, io.SeekStart)
	offsetAndSize, err := d.readUint32()
	if err != nil {
		return 0, 0, err
	}

	offset = int64(4 + ((offsetAndSize >> 5) << 5))
	size = 1 << (offsetAndSize & 0x1f)
	return offset, size, nil
}

// parseNode recursively parses a B-Tree node.
func (d *DSStore) parseNode(allocatorOffset, nodeID uint32) error {
	nodeOffset, _, err := d.getBlockInfo(allocatorOffset, nodeID)
	if err != nil {
		return err
	}
	d.reader.Seek(nodeOffset, io.SeekStart)

	rightmostChildID, err := d.readUint32()
	if err != nil {
		return err
	}
	numRecords, err := d.readUint32()
	if err != nil {
		return err
	}

	var childrenToParse []uint32

	for i := 0; i < int(numRecords); i++ {
		if rightmostChildID != 0 {
			childID, err := d.readUint32()
			if err != nil {
				return err
			}
			childrenToParse = append(childrenToParse, childID)
		}

		nameLen, err := d.readUint32()
		if err != nil {
			return err
		}
		utf16beBytes := make([]byte, nameLen*2)
		if _, err := d.reader.Read(utf16beBytes); err != nil {
			return err
		}
		utf8Decoder := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()
		filenameBytes, _, err := transform.Bytes(utf8Decoder, utf16beBytes)
		if err != nil {
			return err
		}
		filename := string(filenameBytes)

		structBytes := make([]byte, 4)
		if _, err := d.reader.Read(structBytes); err != nil {
			return err
		}
		structID := string(structBytes)

		typeBytes := make([]byte, 4)
		if _, err := d.reader.Read(typeBytes); err != nil {
			return err
		}
		dataType := string(typeBytes)

		data, err := d.parseData(dataType)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping record for '%s' due to parse error: %v\n", filename, err)
			continue
		}

		if _, ok := d.records[filename]; !ok {
			d.records[filename] = make(map[string]interface{})
		}
		d.records[filename][structID] = data
	}

	for _, childID := range childrenToParse {
		if err := d.parseNode(allocatorOffset, childID); err != nil {
			return err
		}
	}
	if rightmostChildID != 0 {
		if err := d.parseNode(allocatorOffset, rightmostChildID); err != nil {
			return err
		}
	}
	return nil
}

// parseData reads a value from the stream based on the provided data type string.
func (d *DSStore) parseData(dataType string) (interface{}, error) {
	switch dataType {
	case "bool":
		val, err := d.reader.ReadByte()
		return val != 0, err
	case "shor", "long":
		return d.readUint32()
	case "comp", "dutc":
		return d.readUint64()
	case "type":
		buf := make([]byte, 4)
		_, err := d.reader.Read(buf)
		return string(buf), err
	case "ustr":
		strLen, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		utf16beBytes := make([]byte, strLen*2)
		if _, err := d.reader.Read(utf16beBytes); err != nil {
			return nil, err
		}
		utf8Decoder := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()
		decoded, _, err := transform.Bytes(utf8Decoder, utf16beBytes)
		return string(decoded), err
	case "blob":
		blobLen, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		blob := make([]byte, blobLen)
		_, err = d.reader.Read(blob)
		return blob, err
	default:
		return nil, fmt.Errorf("unrecognized data type '%s'", dataType)
	}
}

// printHumanReadable formats and prints the parsed records in a human-friendly format.
func (d *DSStore) printHumanReadable() {
	for filename, properties := range d.records {
		fmt.Println(filename)
		for key, val := range properties {
			var output string
			switch key {
			// Safely check the type of `moDD` before using it.
			case "moDD", "modD":
				var unixTime time.Time
				// Check if it's a uint64 (dutc type)
				if timestamp, ok := val.(uint64); ok {
					seconds := int64(timestamp / 65536)
					unixTime = time.Unix(seconds-2082844800, 0)
					output = fmt.Sprintf("Modification date: %s", unixTime.Format("January 2, 2006 at 3:04 PM"))
					// Check if it's a byte slice (blob type)
				} else if blob, ok := val.([]byte); ok {
					// As per research, blob timestamps are often little-endian
					if len(blob) >= 8 {
						timestamp := binary.LittleEndian.Uint64(blob)
						output = fmt.Sprintf("Modification date (from blob): %d", timestamp)
					}
				}
			case "bwsp", "lsvp", "icvp":
				if blob, ok := val.([]byte); ok && bytes.HasPrefix(blob, []byte("bplist")) {
					var plistData interface{}
					decoder := plist.NewDecoder(bytes.NewReader(blob))
					if err := decoder.Decode(&plistData); err == nil {
						xmlBytes, err := plist.MarshalIndent(plistData, plist.XMLFormat, "\t\t")
						if err == nil {
							output = fmt.Sprintf("%s (Property List):\n\t\t%s", key, strings.ReplaceAll(string(xmlBytes), "\n", "\n\t\t"))
						}
					}
				}
			}

			// Default formatting if no special case was met
			if output == "" {
				switch v := val.(type) {
				case []byte:
					output = fmt.Sprintf("%s (blob): 0x%x", key, v)
				default:
					output = fmt.Sprintf("%s: %v", key, v)
				}
			}
			fmt.Printf("\t%s\n", output)
		}
	}
}

// printJSONL formats and prints the parsed records as one JSON object per line.
func (d *DSStore) printJSONL() {
	encoder := json.NewEncoder(os.Stdout)
	for filename, properties := range d.records {
		// Create a new map for the JSON object to ensure a consistent structure
		record := map[string]interface{}{
			"filename":   filename,
			"properties": properties,
		}
		if err := encoder.Encode(record); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON for %s: %v\n", filename, err)
		}
	}
}

func main() {
	outputFormat := flag.String("output", "human", "Output format: 'human' for readable text, 'jsonl' for JSON Lines.")
	flag.Parse()

	// The first argument after the flags is the filename
	filename := ".DS_Store"
	if flag.NArg() > 0 {
		filename = flag.Arg(0)
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file '%s': %v\n", filename, err)
		os.Exit(1)
	}

	dsStore, err := NewDSStore(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing file: %v\n", err)
		os.Exit(1)
	}

	// Choose output format based on the flag
	switch *outputFormat {
	case "jsonl":
		dsStore.printJSONL()
	case "human":
		dsStore.printHumanReadable()
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid output format '%s'. Please use 'human' or 'jsonl'.\n", *outputFormat)
		os.Exit(1)
	}
}
