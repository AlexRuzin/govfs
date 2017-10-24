# govfs
A Virtual Filesystem Library written in the Go programming language. Please note that this project is still under heavy development.

## Synopsis
The govfs Virtual Filesystem is a ext3/4 Unix-based heirarchy type filesystem which begins from the root folder "/". Each file is represented by a meta header which may be referenced in O(1) time, i.e. this filesystem does not use a linked list to create a filesystem "tree", but rather a hash-table which allows for the quick reference of files.

In addition, govfs makes use of golang's gothreads by implementing very quick read/writes. I.E. there is a thread dispatched for each write operation (assuming it is a different file). So writing 1000 files concurrently is possible without breaking the filesystem.

Since this is an in-memory file system, upon unmount the filesystem data is stored in a serialized stream. This, too, is concurrent. So for each file that contains data, a thread will be spawned that will perform compression on the file and serialization of the meta data.

## Features
1. **2^128** files
2. No file size limit
3. Concurrent reads/writes
4. **O(1)** reference time for finding a file header
5. Can print out file lists
6. File metadata serialization upon unmount (i.e. writing the entire filesystem to a physical file)
7. The filesystem file is OS independent
8. Creating one file will automatically create each subdirectory
9. Ideal for projects with heavy filesystem utilization
10. Compression and encryption of the raw filesystem file (unmounted file) is available

## API

### Main Filesystem Header
The FSHeader structure contains all data related to the virtual filesystem
```go
type FSHeader struct {
    filename    string                             /* The raw file on disk */
    key         [16]byte                           /* RC4 key used to encrypt/decrypt the raw file */
    meta        map[string]*gofs_file              /* Hash table containing each file header */
    t_size      uint                               /* Total size of all files */
    [... Other structures/members omitted ...]
}
```

### Create/Load Database
```go
func CreateDatabase(name string, flags int) *FSHeader
```

### Create New File
```go
func (f *FSHeader) Create(name string) (*gofs_file, error)
```

### Create I/O Reader
```go
func (f *FSHeader) NewReader(name string) (*Reader, error)
```

### Standard Read()
```go
func (f *Reader) Read(p []byte) (int, error) 
```

### I/O Reader
```go
func (f *FSHeader) Read(name string) ([]byte, error)
```

### Delete File
```go
func (f *FSHeader) Delete(name string) error
```

### Write to a file
```go
func (f *FSHeader) Write(name string, d []byte) error
```

### Writer interface
```go
type Writer struct {
    Name string
    File *gofs_file
    Hdr *FSHeader
}
```

### New Writer method
```go
func (f *FSHeader) NewWriter(name string) (*Writer, error)
```

### Write method
```go
func (f *Writer) Write(p []byte) (int, error)
```

### Disclaimer
Please see the `LICENSE` file for the detailed MIT license. 
All work written by **Stan Ruzin** _stan_ [dot] _ruzin_ [at] _gmail_ [dot] _com_
