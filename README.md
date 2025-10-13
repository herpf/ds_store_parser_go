# Go DS_Store Parser

A high-performance, command-line tool written in Go to parse and display the contents of macOS `.DS_Store` files in human-readable or JSONL format.

This project is a Go port of the original Python script by Thomas Zhu, designed for speed and portability. Link [here](https://github.com/hanwenzhu/.DS_Store-parser)

The most important benefit is that this tool can be run in the target directly without the need of having installed any non-native tool (such as Python). 

It's important to highlight that I developed this tool mainly using vibe-coding. 



## Features

- **Fast Parsing**: Natively compiled Go provides a significant speed advantage over scripted solutions.
- **Portable**: Generates a single, dependency-free binary that can be run on any modern macOS system.
- **Multiple Output Formats**: Supports both human-friendly text and machine-readable JSONL.
- **Detailed Information**: Extracts filenames, view options, window bounds, modification dates, and other metadata stored in the `.DS_Store` file.

## Installation & Compilation

You must have Go installed on your system.

1.  **Clone the repository:**
    ```sh
    git clone [https://github.com/herpf/ds_store_parser_go.git](https://github.com/herpf/ds_store_parser_go.git)
    cd ds_store_parser_go
    ```

2.  **Fetch dependencies:**
    This command reads the `go.mod` file and downloads the necessary libraries.
    ```sh
    go mod tidy
    ```

3.  **Build the binary:**
    This compiles the source code into a single executable file named `ds_store_parser`.
    ```sh
    go build -o ds_store_parser .
    ```

## Usage

The tool can be run against a `.DS_Store` file. If no file is specified, it defaults to looking for one in the current directory.

### Human-Readable Output (Default)

This format is best for manual inspection.

```sh
# Parse the .DS_Store in the current directory
./ds_store_parser

# Parse a specific file
./ds_store_parser /path/to/some/.DS_Store
```

### JSONL Output

This format outputs one JSON object per line.

```sh
# Parse a specific file and output as JSONL
./ds_store_parser -output jsonl /path/to/some/.DS_Store

# Example with jq to filter for a specific filename
./ds_store_parser -output jsonl ../ds_test3 | jq '. | select(.filename=="untitled folder")'
```

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.