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
// https://golang.org/src/encoding/gob/example_test.go

/* TEST5
 * Supports:
 *  [+] UTF=8 file names <- not yet
 *  [+] 2^128 files
 *  [+] o(1) seek/write time for metadata
 *  [+] There can be two files with the same name, but only if one is a directory
 */

import (
    "os"
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
    _"time"
    _"golang.org/x/crypto/ssh/test"
)

/*
 * Configurable constants
 */
const MAX_FILENAME_LENGTH       int = 256
const FS_SIGNATURE              string = "govfs_header" /* Cannot exceed 64 */
const STREAM_PAD_LEN            int = 0 /* Length of the pad between two serialized RawFile structs */
const REMOVE_FS_HEADER          bool = false /* Removes the header at the beginning of the serialized file */

const IRP_PURGE                 int = 2 /* Flush the entire database and all files */
const IRP_DELETE                int = 3 /* Delete a file/folder */
const IRP_WRITE                 int = 4 /* Write data to a file */
const IRP_CREATE                int = 5 /* Create a new file or folder */

const FLAG_FILE                 int = 1
const FLAG_DIRECTORY            int = 2
const FLAG_COMPRESS             int = 4 /* Compression on the fs serialized output */
const FLAG_ENCRYPT              int = 8 /* Encryption on the fs serialized output */
const FLAG_DB_LOAD              int = 16 /* Loads the database */
const FLAG_DB_CREATE            int = 32 /* Creates the database */

type FSHeader struct {
    filename    string
    key         [16]byte
    meta        map[string]*gofs_file
    t_size      uint /* Total size of all files */
    io_in       chan *gofs_io_block
    create_sync sync.Mutex
    flags       int /* Generic flags as passed in by CreateDatabase() */
}

type gofs_file struct {
    filename    string
    flags       int /* FLAG_FILE, FLAG_DIRECTORY */
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
 * Header which indicates the beginning of the raw filesystem file, written
 *  to the disk.
 */
type raw_stream_hdr struct {
    Signature string /* Uppercase so that it's "exported" i.e. visibile to the encoder */
    FileCount uint
}

/*
 * The meta header for each raw file
 *  (gofs_file is the virtual, in-memory file header)
 */
type RawFile /* Export required for gob serializer */ struct {
    RawSum string
    Flags int
    Name string
    Compressed []byte
    UnzippedLen int
}

/*
 * Creates or loads a filesystem database file. If the filename is nil, then create a new database
 *  otherwise try to load an existing fs database file.
 *
 * Flags: FLAG_ENCRYPT, FLAG_COMPRESS
 */
func CreateDatabase(name string, flags int) (*FSHeader, error) {
    var header *FSHeader

    if (flags & FLAG_DB_LOAD) > 0 {
        /* Check if the file exists */
        if _, err := os.Stat(name); !os.IsNotExist(err) {
            raw, err := read_fs_stream(name, flags)
            if raw == nil || err != nil {
                return nil, err
            }
            header, err = load_header(raw, name)
            if header == nil || err != nil {
                return nil, err
            }
        }
    }

    if (flags & FLAG_DB_CREATE) > 0 {
        /* Either the raw fs does not exist, or it is invalid -- create new */
        header = &FSHeader{
            filename: name,
            meta:     make(map[string]*gofs_file),
        }

        /* Generate the standard "/" file */
        header.meta[s("/")] = new(gofs_file)
        header.meta[s("/")].filename = "/"
    }

    if header == nil {
        return nil, errors.New("error: Invalid header. Failed to generate database header")
    }

    header.flags = flags
    return header, nil
}

func (f *FSHeader) StartIOController() error {
    var header *FSHeader = f

    /* i/o channel processor. Performs i/o to the filesystem */
    header.io_in = make(chan *gofs_io_block)
    go func (f *FSHeader) {
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
                    io.file.flags |= FLAG_DIRECTORY
                } else {
                    io.file.flags |= FLAG_FILE
                }

                /* Recursively create all subdirectory files */
                sub_strings := strings.Split(io.name, "/")
                sub_array := make([]string, len(sub_strings) - 2)
                copy(sub_array, sub_strings[1:len(sub_strings) - 1]) /* We do not need the first/last file */
                var tmp string = ""
                for e := range sub_array {
                    tmp += "/" + sub_array[e]

                    /* Create a subdirectory header */
                    func (sub_directory string, f *FSHeader) {
                        if f := f.check(sub_directory); f != nil {
                            return /* There can exist two files with the same name,
                                       as long as one is a directory and the other is a file */
                        }

                        f.meta[s(tmp)] = new(gofs_file)
                        f.meta[s(tmp)].filename = sub_directory + "/" /* Explicit directory name */
                        f.meta[s(tmp)].flags |= FLAG_DIRECTORY
                    } (tmp, f)
                }

                io.status = nil
                io.io_out <- io
            }
        }
    } (header)

    return nil
}

func (f *FSHeader) check(name string) *gofs_file {
    if sum := s(name); f.meta[sum] != nil {
        return f.meta[sum]
    }

    return nil
}

func (f *FSHeader) generate_irp(name string, data []byte, irp_type int) *gofs_io_block {
    switch irp_type {
    case IRP_DELETE:
        /* DELETE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil /* ERROR -- deleting non-existant file */
        }

        irp := &gofs_io_block {
            file: file_header,
            name: name,
            io_out: make(chan *gofs_io_block),

            operation: IRP_DELETE,
        }

        return irp
    case IRP_WRITE:
        /* WRITE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil
        }

        irp := &gofs_io_block{
            file: file_header,
            name: name,
            data: make([]byte, len(data)),
            io_out: make(chan *gofs_io_block),

            operation: IRP_WRITE, /* write IRP request */
        }
        copy(irp.data, data)

        return irp

    case IRP_CREATE:
        /* CREATE IRP */
        irp := &gofs_io_block{
            name: name,
            operation: IRP_CREATE,
            io_out: make(chan *gofs_io_block),
        }

        return irp
    }

    return nil
}

func (f *FSHeader) Create(name string) (*gofs_file, error) {
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

/*
 * Reader interface
 */
type Reader struct {
    Name string
    File *gofs_file
    Hdr *FSHeader
    Offset int
}

func (f *FSHeader) NewReader(name string) (*Reader, error) {
    file := f.check(name)
    if file == nil {
        return nil, errors.New("error: File not found")
    }

    reader := &Reader{
        Name: name,
        File: file,
        Hdr: f,
        Offset: 0,
    }

    return reader, nil
}

func (f *Reader) Len() (int) {
    return len(f.File.data)
}

func (f *Reader) Read(r []byte) (int, error) {
    if f.Name == "" || f.File == nil || len(f.File.data) < 1  {
        return 0, nil
    }

    data, err := f.Hdr.Read(f.Name)
    if err != nil || len(data) == 0 {
        return 0, err
    }

    if len(r) < len(data) {
        f.Offset += len(r)
        copy(r, data[:len(data) - len(r) - 1])
        return len(data) - len(r) - 1, nil
    }

    /* Sufficient in length, so copy & return EOF */
    copy(r, data)

    return len(data), io.EOF
}

func (f *FSHeader) Read(name string) ([]byte, error) {
    var file_header = f.check(name)
    if file_header == nil {
        return nil, errors.New("read: File does not exist")
    }

    if (file_header.flags & FLAG_DIRECTORY) > 0 {
        return nil, errors.New("read: Cannot read a directory")
    }

    output := make([]byte, len(file_header.data))
    copy(output, file_header.data)
    return output, nil
}

func (f *FSHeader) Delete(name string) error {
    irp := f.generate_irp(name, nil, IRP_DELETE)
    if irp == nil {
        return errors.New("delete: File does not exist") /* ERROR -- File does not exist */
    }

    f.io_in <- irp
    var output_irp = <- irp.io_out
    defer close(irp.io_out)

    return output_irp.status
}

/*
 * Writer interface
 */
type Writer struct {
    Name string
    File *gofs_file
    Hdr *FSHeader
}

func (f *FSHeader) NewWriter(name string) (*Writer, error) {
    file := f.check(name)
    if file == nil {
        return nil, errors.New("error: File not found")
    }

    writer := &Writer {
        Name: name,
        File: file,
        Hdr: f,
    }

    return writer, nil
}

func (f *Writer) Write(p []byte) (int, error) {
    if len(p) < 1 {
        return 0, errors.New("error: Invalid write stream length")
    }

    if err := f.Hdr.Write(f.Name, p); err != nil {
        return 0, err
    }

    return len(p), io.EOF
}

func (f *FSHeader) Write(name string, d []byte) error {
    if i := f.check(name); i == nil {
        return errors.New("write: Cannot write to nonexistent file")
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

func (f *FSHeader) write_internal(d *gofs_file, data []byte) int {
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

func (f *FSHeader) UnmountDB() error {
    type comp_data struct {
        file *gofs_file
        raw RawFile
    }

    commit_ch := make(chan bytes.Buffer)
    for k := range f.meta {
        var channel_header comp_data
        channel_header.file = f.meta[k]
        channel_header.raw = RawFile{
            Flags: f.meta[k].flags,
            RawSum: f.meta[k].datasum,
            Name: f.meta[k].filename,
            UnzippedLen: 0,
        }

        //channel_header.raw.GZIPData = bytes.Buffer{}

       go func (d *comp_data) {
            if d.file.filename == "/" {
                return
            }

            if (d.file.flags & FLAG_FILE) > 0 && len(d.file.data) > 0 {
                d.raw.UnzippedLen = len(d.file.data)

                var zip_buf = bytes.NewBuffer(nil)
                gzip_writer := gzip.NewWriter(zip_buf)
                gzip_writer.Write(d.file.data)

                gzipped := bytes.Buffer{}
                gzipped.ReadFrom(zip_buf)

                d.raw.Compressed = make([]byte, gzipped.Len())
                copy(d.raw.Compressed, gzipped.Bytes())
                gzip_writer.Close()
            } else {
                d.raw.Compressed = nil
            }

            var output = bytes.Buffer{}
            enc := gob.NewEncoder(&output)
            enc.Encode(d.raw)

            commit_ch <- output
        }(&channel_header)
    }

    /* Do not count "/" as a file, since it is not sent in channel */
    total_files := f.get_file_count() - 1

    /*
     * Generate the primary filesystem header and write it to the fs_stream
     */
    hdr := raw_stream_hdr {
        Signature:  FS_SIGNATURE, /* This signature may be modified in the configuration -- FIXME */
        FileCount:  total_files }

    /* Serializer for fs_header */
    var stream *bytes.Buffer

    if REMOVE_FS_HEADER != true {
        stream = func(hdr raw_stream_hdr) *bytes.Buffer {
            b := new(bytes.Buffer)
            e := gob.NewEncoder(b)
            if err := e.Encode(hdr); err != nil {
                return nil /* Failure in encoding the fs_header structure -- Should not happen */
            }

            return b
        }(hdr)
    } else {
        stream = new(bytes.Buffer)
    }

    /* serialized RawFile metadata includes the gzip'd file data, if necessary */
    for total_files != 0 {
        var meta_raw = <- commit_ch
        stream.Write(meta_raw.Bytes())
        total_files -= 1
    }

    close(commit_ch)

    /* Compress, encrypt, and write stream */
    written, err := f.write_fs_stream(f.filename, stream, f.flags)
    if err != nil || int(written) == 0 {
        return errors.New("error: Failure in writing raw fs stream")
    }

    return err
}

func load_header(data []byte, filename string) (*FSHeader, error) {
    ptr := bytes.NewBuffer(data) /* raw file stream */

    if REMOVE_FS_HEADER != true {
        header, err := func(p *bytes.Buffer) (*raw_stream_hdr, error) {
            output := new(raw_stream_hdr)

            d := gob.NewDecoder(p)
            err := d.Decode(output)
            if err != nil {
                return nil, err
            }

            return output, nil
        }(ptr)

        if err != nil || header == nil || header.Signature != FS_SIGNATURE {
            return nil, err
        }
    }

    output := &FSHeader{
        filename: filename,
        meta:     make(map[string]*gofs_file),
    }
    output.meta[s("/")] = new(gofs_file)
    output.meta[s("/")].filename = "/"

    /* Enumerate files */
    for {
        file_hdr, err := func (p *bytes.Buffer) (*RawFile, error) {
            output := &RawFile{}

            d := gob.NewDecoder(p)
            err := d.Decode(output)
            if err != nil && err != io.EOF {
                return nil, err
            }

            for i := STREAM_PAD_LEN; i != 0; i -= 1 {
                p.UnreadByte()
            }

            return output, nil
        } (ptr)

        if err != nil {
            return nil, err
        }

        output.meta[s(file_hdr.Name)] = &gofs_file{
            filename: file_hdr.Name,
            flags: file_hdr.Flags,
            data: nil,
            datasum: "",
        }

        //output.meta[s(file_hdr.Name)].data = make([]byte, decompressed_len)
        if file_hdr.Compressed != nil {
            output.meta[s(file_hdr.Name)].datasum = file_hdr.RawSum
            output.meta[s(file_hdr.Name)].data = make([]byte, file_hdr.UnzippedLen)

            zipped := bytes.NewBuffer(file_hdr.Compressed)
            gzip, err := gzip.NewReader(zipped)
            if err != nil {
                return nil, err
            }


            decompressed_len, err := gzip.Read(output.meta[s(file_hdr.Name)].data)
            if decompressed_len != file_hdr.UnzippedLen || err != nil {
                return nil, err
            }
        }
    }

    return output, nil
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
 *  serialized fs table. Since no FSHeader exists yet, this method will not be apart of that
 *  structure, as per design choice
 */
func read_fs_stream(name string, flags int) ([]byte, error) {
    if _, err := os.Stat(name); os.IsNotExist(err) {
        return nil, err
    }

    raw_file, err := ioutil.ReadFile(name)
    if err != nil {
        return nil, err
    }

    var plaintext []byte

    if (flags & FLAG_ENCRYPT) > 0 {
        /* The crypto key is composed of the MD5 of the hostname + the FS_SIGNATURE */
        key := get_fs_key()

        plaintext, err = crypto.RC4_Decrypt(raw_file, &key)
        if err != nil {
            return nil, err
        }
    } else {
        plaintext = make([]byte, len(raw_file))
        copy(plaintext, raw_file)
    }

    var decompressed []byte

    if (flags & FLAG_COMPRESS) > 0 {
        var b bytes.Buffer
        b.Read(plaintext)

        reader, err := gzip.NewReader(&b)
        defer reader.Close()

        decompressed, err = ioutil.ReadAll(reader)
        if err != nil {
            return nil, err
        }
    } else {
        decompressed = make([]byte, len(plaintext))
        copy(decompressed, plaintext)
    }

    return decompressed, nil
}

/*
 * Takes in the serialized fs table, compresses it, encrypts it and writes it to the disk
 */
func (f *FSHeader) write_fs_stream(name string, data *bytes.Buffer, flags int) (uint, error) {

    var compressed = new(bytes.Buffer)

    if (flags & FLAG_COMPRESS) > 0 {
        w := gzip.NewWriter(compressed)
        w.Write(data.Bytes())
        w.Close()
    } else {
        compressed.Write(data.Bytes())
    }

    var ciphertext []byte

    if (flags & FLAG_ENCRYPT) > 0 {
        /* The crypto key will be the MD5 of the hostname string + the FS_SIGNATURE string */
        key := get_fs_key()

        /* Perform RC4 encryption */
        var err error
        ciphertext, err = crypto.RC4_Encrypt(data.Bytes(), &key)
        if err != nil {
            return 0, err
        }
    } else {
        ciphertext = make([]byte, compressed.Len())
        copy(ciphertext, compressed.Bytes())
    }

    if _, err := os.Stat(name); os.IsExist(err) {
        os.Remove(name)
    }

    file, err := os.Create(name)
    if err != nil {
        return 0, err
    }
    defer file.Close()

    written, err := file.Write(ciphertext)
    if err != nil {
        return uint(written), err
    }

    return uint(written), nil
}

func (f *FSHeader) get_file_count() uint {
    var total uint = 0
    for range f.meta {
        total += 1
    }

    return total
}

func (f *FSHeader) get_file_size(name string) (uint, error) {
    file := f.check(name)
    if file == nil {
        return 0, errors.New("get_file_size: File does not exist")
    }

    return uint(len(file.data)), nil
}

func (f *FSHeader) get_total_filesizes() uint {
    return f.t_size
}

func (f *FSHeader) get_file_list() []string {
    var output []string

    for k := range f.meta {
        file := f.meta[k]
        if (file.flags & FLAG_DIRECTORY) > 0 {
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

/* EOF */

