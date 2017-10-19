/*
 * Copyright (c) 2017 AlexRuzin (stan.ruzin@gmail.com)
 * 
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 * 
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 * 
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package govfs

// TODO
// create() can either create a folder or a file. 
// When a folder/file is created, make all subdirectories in the map as well

/* TEST5
 * Supports:
 *  [+] UTF=8 file names <- not yet
 *  [+] 2^128 files
 *  [+] o(1) seek/write time for metadata
 *  [+] There can be two files with the same name, but only if one is a directory
 */

import (
    "os"
    "fmt"
    "crypto/md5"
    "encoding/hex"
    "encoding/gob"
    "compress/gzip"
    "bytes"
    "sync"
    "strings"
    "errors"
    "github.com/AlexRuzin/crypto"
    "io"
    "io/ioutil"
)

/*
 * Configurable constants
 */
const MAX_FILENAME_LENGTH       int = 256
const FS_SIGNATURE              string = "govfs_header" /* Cannot exceed 64 */

const IRP_PURGE                 int = 2 /* Flush the entire database and all files */
const IRP_DELETE                int = 3 /* Delete a file/folder */
const IRP_WRITE                 int = 4 /* Write data to a file */
const IRP_CREATE                int = 5 /* Create a new file or folder */

const FLAG_FILE                 int = 1
const FLAG_DIRECTORY            int = 2
const FLAG_COMPRESS             int = 4 /* Compression on the fs serialized output */
const FLAG_ENCRYPT              int = 8 /* Encryption on the fs serialized output */

type gofs_header struct {
    filename    string
    key         [16]byte
    meta        map[string]*gofs_file
    t_size      uint /* Total size of all files */
    io_in       chan *gofs_io_block
    create_sync sync.Mutex
}

type gofs_file struct {
    filename    string
    filetype    int /* FLAG_FILE, FLAG_DIRECTORY */
    datasum     string
    data        []byte
    lock        sync.Mutex
}

type gofs_io_block struct {
    file        *gofs_file
    name        string
    data        []byte
    status      error
    operation   int /* 2 == purge, 3 == delete, 4 == write */
    flags       int
    io_out      chan *gofs_io_block
}

/*
 * Creates or loads a filesystem database file. If the filename is nil, then create a new database
 *  otherwise try to load an existing fs database file.
 */
func create_db(filename *string) *gofs_header {
    var header *gofs_header

    if filename != nil {
        /* Check if the file exists */
        _, err := os.Stat(*filename)
        if os.IsExist(err) {
            raw, _ := read_fs_stream(*filename, FLAG_ENCRYPT|FLAG_COMPRESS)
            header, _ = load_header(raw)
        }
    }

    if header == nil {
        /* Either the raw fs does not exist, or it is invalid -- create new */
        header = new(gofs_header)
        header.filename = *filename
        header.meta = make(map[string]*gofs_file)
        header.meta[s("/")] = new(gofs_file)
        header.meta[s("/")].filename = "/"
    } /* test change */

    /* i/o channel processor. Performs i/o to the filesystem */
    header.io_in = make(chan *gofs_io_block)
    go func (f *gofs_header) {
        for {
            var io = <- header.io_in
            
            switch io.operation {
            case IRP_PURGE:
                /* PURGE */
                out("ERROR: PURGING")
                close(header.io_in)
                return
            case IRP_DELETE:
                /* DELETE */
                // FIXME/ADDME
                io.status = errors.New("IRP_DELETE generic error")
                if io.file.filename == "/" { /* Cannot delete the root file */
                    io.status = errors.New("IRP_DELETE: Tried to delete the root file")
                    io.io_out <- io
                } else {
                    if i := f.check(io.name); i != nil {
                        delete(f.meta, s(io.name))
                        f.meta[s(io.name)] = nil
                        io.status = nil
                    }
                    io.io_out <- io
                }
            case IRP_WRITE:
                /* WRITE */
                if i := f.check(io.name); i != nil {
                    io.file.lock.Lock()
                    if f.write_internal(i, io.data) == len(io.data) {
                        io.status = nil
                        io.file.lock.Unlock()
                        io.io_out <- io
                    } else {
                        io.status = errors.New("IRP_WRITE: Failed to write to filesystem")
                        io.file.lock.Unlock()
                        io.io_out <- io
                    }
                }
            case IRP_CREATE:          
                f.meta[s(io.name)] = new(gofs_file)
                io.file = f.meta[s(io.name)]                
                io.file.filename = io.name
                
                if string(io.name[len(io.name) - 1:]) == "/" {
                    io.file.filetype = FLAG_DIRECTORY
                } else {
                    io.file.filetype = FLAG_FILE
                }
                
                /* Recursively create all subdirectory files */
                sub_strings := strings.Split(io.name, "/")
                sub_array := make([]string, len(sub_strings) - 2)
                copy(sub_array, sub_strings[1:len(sub_strings) - 1]) /* We do not need the first/last file */
                var tmp string = ""
                for e := range sub_array {
                    tmp += "/" + sub_array[e]

                    /* Create a subdirectory header */
                    func (sub_directory string, f *gofs_header) {
                        if f := f.check(sub_directory); f != nil {
                            return /* There can exist two files with the same name,
                                       as long as one is a directory and the other is a file */
                        }

                        f.meta[s(tmp)] = new(gofs_file)
                        f.meta[s(tmp)].filename = sub_directory + "/" /* Explicit directory name */
                        f.meta[s(tmp)].filetype = FLAG_DIRECTORY
                    } (tmp, f)
                }

                io.status = nil
                io.io_out <- io
            }
        }
    } (header)

    return header
}

func (f *gofs_header) check(name string) *gofs_file {
    if sum := s(name); f.meta[sum] != nil {
        return f.meta[sum]
    }

    return nil
}

func (f *gofs_header) generate_irp(name string, data []byte, irp_type int) *gofs_io_block {
    switch irp_type {
    case IRP_DELETE:
        /* DELETE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil /* ERROR -- deleting non-existant file */
        }

        irp := new(gofs_io_block)
        irp.file = file_header
        irp.name = name
        irp.io_out = make(chan *gofs_io_block)

        irp.operation = IRP_DELETE

        return irp
    case IRP_WRITE:
        /* WRITE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil
        }

        irp := new(gofs_io_block)
        irp.file = file_header
        irp.name = name
        irp.data = make([]byte, len(data))
        irp.io_out = make(chan *gofs_io_block)
        copy(irp.data, data)

        irp.operation = IRP_WRITE /* write IRP request */

        return irp
        
    case IRP_CREATE:
        /* CREATE IRP */
        irp := new(gofs_io_block)
        irp.name = name
        irp.operation = IRP_CREATE
        irp.io_out = make(chan *gofs_io_block)
        
        return irp
    }    
    
    return nil
}

func (f *gofs_header) create(name string) (*gofs_file, error) {
    if file := f.check(name); file != nil {
        return nil, errors.New("create: File already exists")
    }

    if len(name) > MAX_FILENAME_LENGTH {
        return nil, errors.New("create: File name is too long")
    }

    f.create_sync.Lock()
    var irp *gofs_io_block = f.generate_irp(name, nil, IRP_CREATE)
    
    f.io_in <- irp
    output_irp := <- irp.io_out
    f.create_sync.Unlock()
    if output_irp.file == nil {
        return nil, output_irp.status
    }
    close(output_irp.io_out)

    return output_irp.file, nil
}

func (f *gofs_header) read(name string) ([]byte, error) {
    var file_header = f.check(name)
    if file_header == nil {
        return nil, errors.New("read: File does not exist")
    }

    if file_header.filetype == FLAG_DIRECTORY {
        return nil, errors.New("read: Cannot read a directory")
    }

    output := make([]byte, len(file_header.data))
    copy(output, file_header.data)
    return output, nil
}

func (f *gofs_header) delete(name string) error {
    irp := f.generate_irp(name, nil, IRP_DELETE)
    if irp == nil {
        return errors.New("delete: File does not exist") /* ERROR -- File does not exist */
    }

    f.io_in <- irp
    var output_irp = <- irp.io_out
    defer close(irp.io_out)

    return output_irp.status
}

func (f *gofs_header) write(name string, d []byte) error {
    if i := f.check(name); i == nil {
        return errors.New("write: Cannot write to nonexistant file")
    }
    
    irp := f.generate_irp(name, d, IRP_WRITE)
    if irp == nil {
        return errors.New("write: Failed to generate IRP_WRITE") /* FAILURE */
    }

    /*
     * Send the write request IRP and receive the response
     *  IRP indicating the write status of the request
     */
    f.io_in <- irp
    var output_irp = <- irp.io_out
    defer close(irp.io_out)

    return output_irp.status
}

func (f *gofs_header) write_internal(d *gofs_file, data []byte) int {
    if len(data) == 0 {
        return len(data)
    }

    if uint(len(data)) >= uint(len(d.data)) {
        f.t_size += uint(len(data)) - uint(len(d.data))
    } else {
        f.t_size -= uint(len(d.data)) - uint(len(data))
    }

    d.data = make([]byte, len(data))
    copy(d.data, data)
    d.datasum = s(string(data))

    datalen := len(d.data)

    return datalen
}

func (f *gofs_header) unmount_db() error {
    type RawFile /* Capitalize for the sake of exporting */ struct {
        RawSum [16]byte
        GZIPSize uint
        Flags int
        Name [MAX_FILENAME_LENGTH]byte
    }

    type comp_data struct {
        file *gofs_file
        data_compressed []byte
        raw RawFile
    }

    commit_ch := make(chan *comp_data)
    for k := range f.meta {
        header := new(comp_data)
        header.file = f.meta[k]

        go func (d *comp_data) {
            if d.file.filename == "/" {
                return
            }

            /*
             * Perform compression of the file, and store it in 'd'
             */
            if d.file.filetype == FLAG_FILE /* File */ && len(d.file.data) > 0 {
                /* Compression required since this is a file, and it's length is > 0 */
                buf := func (data []byte) *bytes.Buffer {
                    var output = new(bytes.Buffer)
                    w := gzip.NewWriter(output)
                    w.Write(d.file.data)
                    w.Close()

                    return output
                } (d.file.data)

                d.data_compressed = make([]byte, buf.Len())
                buf.Write(d.data_compressed)

                d.raw.RawSum = md5.Sum(d.file.data)
                d.raw.GZIPSize = uint(len(d.data_compressed))
                d.raw.Flags = FLAG_FILE
                copy(d.raw.Name[:], d.file.filename)

                commit_ch <- d
            }

            if d.file.filetype == FLAG_DIRECTORY {
                /* Directory type file. No need for compression, but the metadata must exist */
                d.raw.Flags = FLAG_DIRECTORY
                copy(d.raw.Name[:], d.file.filename)
                commit_ch <- d
            }

            if d.file.filetype == FLAG_FILE && len(d.file.data) == 0 {
                /* Empty file. Does not need compression but metadata must exist */
                d.raw.Flags = FLAG_FILE
                copy(d.raw.Name[:], d.file.filename)
                commit_ch <- d
            }
        }(header)
    }

    /* Do not count "/" as a file, since it is not sent in channel */
    total_files := f.get_file_count() - 1

    /*
     * Generate the primary filesystem header and write it to the fs_stream
     */
    type fs_header struct {
        Signature string /* Uppercase so that it's "exported" i.e. visibile to the encoder */
        FileCount uint
    }
    hdr := fs_header {
        Signature:  FS_SIGNATURE, /* This signature may be modified in the configuration -- FIXME */
        FileCount:  total_files }

    /* Serializer for fs_header */
    stream := func (object interface{}) *bytes.Buffer {
        b := new(bytes.Buffer)
        e := gob.NewEncoder(b)
        if err := e.Encode(object); err != nil {
            return nil /* Failure in encoding the fs_header structure -- Should not happen */
        }

        return b
    } (hdr)

    for total_files != 0 {
        var header = <- commit_ch

        /* Append the header */
        serialized_fileheader := func (object interface{}) *bytes.Buffer {
            b := new(bytes.Buffer)
            e := gob.NewEncoder(b)
            if err := e.Encode(object); err != nil {
                return nil /* This should be an assertion -- FIXME */
            }
            return b
        } (header.raw) /* Pass in RawFile */
        stream.Write(serialized_fileheader.Bytes())

        /* Append the compressed data */
        stream.Write(header.data_compressed)

        total_files -= 1
    }

    close(commit_ch)

    /* Compress, encrypt, and write stream */
    _, err := f.write_fs_stream(f.filename, stream, FLAG_COMPRESS | FLAG_ENCRYPT)

    return err
}

func load_header(data []byte) (*gofs_header, error) {
    out(string(data))
    return nil, errors.New("Unknown error")
}

/*
 * Generate the key used to encrypt/decrypt the raw fs table. The key is composed of the
 *  MD5 sum of the hostname + the FS_SIGNATURE string
 */
func get_fs_key() []byte {
    host, _ := os.Hostname()
    host += FS_SIGNATURE

    sum := md5.Sum([]byte(host))
    output := make([]byte, len(sum))
    copy(output, sum[:])
    return output
}

/*
 * Decrypts the raw fs stream from a filename, decompresses it, and returns a vector composed of the
 *  serialized fs table. Since no gofs_header exists yet, this method will not be apart of that
 *  structure, as per design choice
 */
func read_fs_stream(name string, flags int) ([]byte, error) {
    if flags != FLAG_COMPRESS | FLAG_ENCRYPT {
        return nil, errors.New("read_fs_stream: Operation not implemented")
    }

    if _, err := os.Stat(name); os.IsNotExist(err) {
        return nil, errors.New("error: Cannot read from fs stream. File does not exist")
    }

    file, err := os.Create(name)
    if err != nil {
        return nil, errors.New("error: Cannot read from fs stream.")
    }
    defer file.Close()

    raw_file := bytes.NewBuffer(nil)
    io.Copy(raw_file, file)

    /* The crypto key is composed of the MD5 of the hostname + the FS_SIGNATURE */
    key := get_fs_key()

    plaintext, err := crypto.RC4_Decrypt(raw_file.Bytes(), &key)
    if err != nil {
        return nil, errors.New("error: Failed to decrypt raw fs stream")
    }

    var b bytes.Buffer
    b.Read(plaintext)

    reader, err := gzip.NewReader(&b)
    defer reader.Close()

    decompressed, err := ioutil.ReadAll(reader)
    if err != nil {
        return nil, errors.New("error: Failed to decompress fs stream")
    }

    return decompressed, nil
}

/*
 * Takes in the serialized fs table, compresses it, encrypts it and writes it to the disk
 */
func (f *gofs_header) write_fs_stream(name string, data *bytes.Buffer, flags int) (uint, error) {
    if flags != FLAG_ENCRYPT | FLAG_COMPRESS {
        return 0, errors.New("read_fs_stream: Operation not implemented") // FIXME
    }

    var compressed *bytes.Buffer = new(bytes.Buffer)
    w := gzip.NewWriter(compressed)
    w.Write(data.Bytes())
    w.Close()

    /* The crypto key will be the MD5 of the hostname string + the FS_SIGNATURE string */
    key := get_fs_key()

    /* Perform RC4 encryption */
    ciphertext, err := crypto.RC4_Encrypt(data.Bytes(), &key)
    if err != nil {
        return 0, errors.New("error: Failure in invoking RC4 cipher on raw rs table")
    }

    if _, err := os.Stat(name); os.IsExist(err) {
        os.Remove(name)
    }

    file, err := os.Create(name)
    if err != nil {
        return 0, errors.New("write_fs_stream: Failed to create database file")
    }
    defer file.Close()

    written, err := file.Write(ciphertext)
    if err != nil {
        return uint(written), errors.New("write_fs_stream: Failed to write database file")
    }

    return uint(written), nil
}

func (f *gofs_header) get_file_count() uint {
    var total uint = 0
    for range f.meta {
        total += 1
    }

    return total
}

func (f *gofs_header) get_file_size(name string) (uint, error) {
    file := f.check(name)
    if file == nil {
        return 0, errors.New("get_file_size: File does not exist")
    }

    return uint(len(file.data)), nil
}

func (f *gofs_header) get_total_filesizes() uint {
    return f.t_size
}

func (f *gofs_header) get_file_list() []string {
    var output []string

    for k := range f.meta {
        file := f.meta[k]
        if file.filetype == FLAG_DIRECTORY {
            output = append(output, "(DIR)  " + file.filename)
            continue
        }
        output = append(output, "(FILE) " + file.filename)
    }

    return output
}

/* Returns an md5sum of a string */
func s(name string) string {
    name_seeded := name + "gofs_magic"
    d := make([]byte, len(name_seeded))
    copy(d, name_seeded)
    sum := md5.Sum(d)
    return hex.EncodeToString(sum[:])
}

func out(debug string) {
    fmt.Println(debug)
}

func out_hex(debug []byte) {
    fmt.Printf("%v\r\n", debug)
}
