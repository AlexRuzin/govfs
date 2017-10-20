# govfs
A Virtual Filesystem Library written in the Go programming language. Please note that this project is still under heavy development.

# Synopsis
The govfs Virtual Filesystem is a ext3/4 Unix-based heirarchy type filesystem which begins from the root folder "/". Each file is represented by a meta header which may be referenced in O(1) time, i.e. this filesystem does not use a linked list to create a filesystem "tree", but rather a hash-table which allows for the quick reference of files.

In addition, govfs makes use of golang's gothreads by implementing very quick read/writes. I.E. there is a thread dispatched for each write operation (assuming it is a different file). So writing 1000 files concurrently is possible without breaking the filesystem.

Since this is an in-memory file system, upon unmount the filesystem data is stored in a serialized stream. This, too, is concurrent. So for each file that contains data, a thread will be spawned that will perform compression on the file and serialization of the meta data.

# Features
>> 2^128 files
>> No file size limit
>> Concurrent reads/writes
>> O(1) reference time for finding a file header
>> Can print out file lists
>> File metadata serialization upon unmount (i.e. writing the entire filesystem to a physical file)
>> The filesystem file is OS independent
>> Creating one file will automatically create each subdirectory
>> Ideal for projects with heavy filesystem utilization
>> Compression and encryption of the raw filesystem file (unmounted file) is available

# API

The FSHeader structure contains all data related to the virtual filesystem
type FSHeader struct {
    filename    string /* The raw file on disk */
    key         [16]byte /* RC4 key used to encrypt/decrypt the raw file */
    meta        map[string]*gofs_file /* Hash table containing each file header */
    t_size      uint /* Total size of all files */
    [... Other structures/members omitted ...]
}

/* Creates the database either by loading a raw file, or by generating a new one */
func CreateDatabase(name string, flags int) *FSHeader

/* Create a new file */
func (f *FSHeader) Create(name string) (*gofs_file, error)

/* The Reader interface -- Create a Reader */
func (f *FSHeader) NewReader(name string) (*Reader, error)

/* Read from a file using the Reader */
func (f *Reader) Read(p []byte) (int, error) 

/* Read from a file without the Reader */
func (f *FSHeader) Read(name string) ([]byte, error)

/* Delete a file */
func (f *FSHeader) Delete(name string) error

/* Write to a file */
func (f *FSHeader) Write(name string, d []byte) error
