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
// Fix all FLAG_COMPRESS operations -- causes a panic in CreateDatabase()

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
    "github.com/AlexRuzin/cryptog"
    "io"
    "io/ioutil"
    "github.com/AlexRuzin/util"
)

/*
 * Configurable constants
 */
const MAX_FILENAME_LENGTH     int       = 256
const FS_SIGNATURE            string    = "govfs_header"    /* Cannot exceed 64 */
const STREAM_PAD_LEN          int       = 0                 /* Length of the pad between two serialized RawFile structs */
const REMOVE_FS_HEADER        bool      = false             /* Removes the header at the beginning of the serialized file - leave false */

type FlagVal int
const IRP_BASE                FlagVal = 2 /* Start the IRP controller ID count from n */
const (
    IRP_PURGE                 FlagVal = IRP_BASE + iota /* Flush the entire database and all files */
    IRP_DELETE                /* Delete a file/folder */
    IRP_WRITE                 /* Write data to a file */
    IRP_CREATE                /* Create a new file or folder */
)

const (
    FLAG_FILE                 FlagVal = 1 << iota
    FLAG_DIRECTORY            /* The target file is a directory */
    FLAG_COMPRESS             /* Compression on the fs serialized output */
    FLAG_ENCRYPT              /* Encryption on the fs serialized output */
    FLAG_DB_LOAD              /* Loads the database */
    FLAG_DB_CREATE            /* Creates the database */
    FLAG_COMPRESS_FILES       /* Compresses files in the FS stream */
)

type FSHeader struct {
    filename    string
    key         [16]byte
    meta        map[string]*govfsFile
    t_size      uint /* Total size of all files */
    io_in       chan *govfsIoBlock
    create_sync sync.Mutex
    flags       FlagVal /* Generic flags as passed in by CreateDatabase() */
}

type govfsFile struct {
    filename    string
    flags       FlagVal /* FLAG_FILE, FLAG_DIRECTORY */
    datasum     string
    data        []byte
    lock        sync.Mutex
}

type govfsIoBlock struct {
    file        *govfsFile
    name        string
    data        []byte
    status      error
    operation   FlagVal /* 2 == purge, 3 == delete, 4 == write */
    flags       FlagVal
    io_out      chan *govfsIoBlock
}

/*
 * Header which indicates the beginning of the raw filesystem file, written
 *  to the disk.
 */
type rawStreamHeader struct {
    Signature string /* Uppercase so that it's "exported" i.e. visibile to the encoder */
    FileCount uint
}

/*
 * The meta header for each raw file
 *  (govfsFile is the virtual, in-memory file header)
 */
type RawFile /* Export required for gob serializer */ struct {
    RawSum string
    Flags FlagVal
    Name string
    UnzippedLen int
}

/*
 * Creates or loads a filesystem database file. If the filename is nil, then create a new database
 *  otherwise try to load an existing fs database file.
 *
 * Flags: FLAG_ENCRYPT, FLAG_COMPRESS
 */
func CreateDatabase(name string, flags FlagVal) (*FSHeader, error) {
    var header *FSHeader

    if (flags & FLAG_DB_LOAD) > 0 {
        /* Check if the file exists */
        if _, err := os.Stat(name); !os.IsNotExist(err) {
            raw, err := readFsStream(name, flags)
            if raw == nil || err != nil {
                return nil, err
            }
            header, err = loadHeader(raw, name)
            if header == nil || err != nil {
                return nil, err
            }
        }
    }

    if (flags & FLAG_DB_CREATE) > 0 {
        /* Either the raw fs does not exist, or it is invalid -- create new */
        header = &FSHeader{
            filename: name,
            meta:     make(map[string]*govfsFile),
        }

        /* Generate the standard "/" file */
        header.meta[s("/")] = new(govfsFile)
        header.meta[s("/")].filename = "/"
        header.t_size = 0
    }

    if header == nil {
        return nil, util.RetErrStr("Invalid header. Failed to generate database header")
    }

    header.flags = flags
    return header, nil
}

func (f *FSHeader) StartIOController() error {
    var header *FSHeader = f

    /* i/o channel processor. Performs i/o to the filesystem */
    header.io_in = make(chan *govfsIoBlock)
    go func (f *FSHeader) {
        for {
            var ioh = <- header.io_in

            switch ioh.operation {
            case IRP_PURGE:
                /* PURGE */
                ioh.status = util.RetErrStr("Purge command issued")
                close(header.io_in)
                return
            case IRP_DELETE:
                /* DELETE */
                // FIXME/ADDME
                ioh.status = util.RetErrStr("IRP_DELETE generic error")
                if ioh.file.filename == "/" { /* Cannot delete the root file */
                    ioh.status = util.RetErrStr("IRP_DELETE: Tried to delete the root file")
                    ioh.io_out <- ioh
                } else {
                    if i := f.check(ioh.name); i != nil {
                        delete(f.meta, s(ioh.name))
                        f.meta[s(ioh.name)] = nil
                        ioh.status = nil
                    }
                    ioh.io_out <- ioh
                }
            case IRP_WRITE:
                /* WRITE */
                if i := f.check(ioh.name); i != nil {
                    ioh.file.lock.Lock()
                    if f.writeInternal(i, ioh.data) == len(ioh.data) {
                        ioh.status = nil
                        ioh.file.lock.Unlock()
                        ioh.io_out <- ioh
                    } else {
                        ioh.status = util.RetErrStr("IRP_WRITE: Failed to write to filesystem")
                        ioh.file.lock.Unlock()
                        ioh.io_out <- ioh
                    }
                }
            case IRP_CREATE:
                f.meta[s(ioh.name)] = new(govfsFile)
                ioh.file = f.meta[s(ioh.name)]
                ioh.file.filename = ioh.name

                if string(ioh.name[len(ioh.name) - 1:]) == "/" {
                    ioh.file.flags |= FLAG_DIRECTORY
                } else {
                    ioh.file.flags |= FLAG_FILE
                }

                /* Recursively create all subdirectory files */
                sub_strings := strings.Split(ioh.name, "/")
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

                        f.meta[s(tmp)] = new(govfsFile)
                        f.meta[s(tmp)].filename = sub_directory + "/" /* Explicit directory name */
                        f.meta[s(tmp)].flags |= FLAG_DIRECTORY
                    } (tmp, f)
                }

                ioh.status = nil
                ioh.io_out <- ioh
            }
        }
    } (header)

    return nil
}

/*
 * Exported method to check for object existence in db
 */
func (f *FSHeader) Check(name string) *govfsFile {
    return f.check(name)
}

func (f *FSHeader) check(name string) *govfsFile {
    if sum := s(name); f.meta[sum] != nil {
        return f.meta[sum]
    }

    return nil
}

func (f *FSHeader) generateIRP(name string, data []byte, irp_type FlagVal) *govfsIoBlock {
    switch irp_type {
    case IRP_DELETE:
        /* DELETE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil /* ERROR -- deleting non-existant file */
        }

        irp := &govfsIoBlock {
            file: file_header,
            name: name,
            io_out: make(chan *govfsIoBlock),

            operation: IRP_DELETE,
        }

        return irp
    case IRP_WRITE:
        /* WRITE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil
        }

        irp := &govfsIoBlock{
            file: file_header,
            name: name,
            data: make([]byte, len(data)),
            io_out: make(chan *govfsIoBlock),

            operation: IRP_WRITE, /* write IRP request */
        }
        copy(irp.data, data)

        return irp

    case IRP_CREATE:
        /* CREATE IRP */
        irp := &govfsIoBlock{
            name: name,
            operation: IRP_CREATE,
            io_out: make(chan *govfsIoBlock),
        }

        return irp
    }

    return nil
}

func (f *FSHeader) Create(name string) (*govfsFile, error) {
    if file := f.check(name); file != nil {
        return nil, util.RetErrStr("create: File already exists")
    }

    if len(name) > MAX_FILENAME_LENGTH {
        return nil, util.RetErrStr("create: File name is too long")
    }

    f.create_sync.Lock()
    var irp *govfsIoBlock = f.generateIRP(name, nil, IRP_CREATE)

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
    File *govfsFile
    Hdr *FSHeader
    Offset int
}

func (f *FSHeader) NewReader(name string) (*Reader, error) {
    file := f.check(name)
    if file == nil {
        return nil, util.RetErrStr("File not found")
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
        return nil, util.RetErrStr("read: File does not exist")
    }

    if (file_header.flags & FLAG_DIRECTORY) > 0 {
        return nil, util.RetErrStr("read: Cannot read a directory")
    }

    output := make([]byte, len(file_header.data))
    copy(output, file_header.data)
    return output, nil
}

func (f *FSHeader) Delete(name string) error {
    irp := f.generateIRP(name, nil, IRP_DELETE)
    if irp == nil {
        return util.RetErrStr("delete: File does not exist") /* ERROR -- File does not exist */
    }

    f.io_in <- irp
    var output_irp = <- irp.io_out
    defer close(irp.io_out)

    return output_irp.status
}

/*
 * Commits in-memory objects to the disk
 */
func (f *FSHeader) Commit() (*FSHeader, error) {
    f.UnmountDB(0)

    if _, err := os.Stat(f.filename); os.IsNotExist(err) {
        return nil, err
    }

    var header, err = CreateDatabase(f.filename, FLAG_DB_LOAD)
    if err != nil {
        return nil, err
    }

    if err := header.StartIOController(); err != nil {
        return nil, err
    }

    return header, nil
}

/*
 * Writer interface
 */
type Writer struct {
    Name string
    File *govfsFile
    Hdr *FSHeader
}

func (f *FSHeader) NewWriter(name string) (*Writer, error) {
    file := f.check(name)
    if file == nil {
        return nil, util.RetErrStr("File not found")
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
        return 0, util.RetErrStr("Invalid write stream length")
    }

    if err := f.Hdr.Write(f.Name, p); err != nil {
        return 0, err
    }

    return len(p), io.EOF
}

func (f *FSHeader) Write(name string, d []byte) error {
    if i := f.check(name); i == nil {
        return util.RetErrStr("write: Cannot write to nonexistent file")
    }

    irp := f.generateIRP(name, d, IRP_WRITE)
    if irp == nil {
        return util.RetErrStr("write: Failed to generate IRP_WRITE") /* FAILURE */
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

func (f *FSHeader) writeInternal(d *govfsFile, data []byte) int {
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

func (f *FSHeader) UnmountDB(flags FlagVal /* FLAG_COMPRESS_FILES */) error {
    type comp_data struct {
        file *govfsFile
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

        go func (d *comp_data) {
            if d.file.filename == "/" {
                return
            }

            var data_stream []byte
            if (d.file.flags & FLAG_FILE) > 0 && len(d.file.data) > 0 {
                d.raw.UnzippedLen = len(d.file.data)

                if (flags & FLAG_COMPRESS_FILES) > 0 {
                    d.raw.Flags |= FLAG_COMPRESS_FILES

                    var zip_buf= bytes.NewBuffer(nil)
                    gzip_writer := gzip.NewWriter(zip_buf)
                    gzip_writer.Write(d.file.data)

                    gzipped := bytes.Buffer{}
                    gzipped.ReadFrom(zip_buf)

                    data_stream = make([]byte, gzipped.Len())
                    copy(data_stream, gzipped.Bytes())
                    gzip_writer.Close()
                } else {
                    data_stream = make([]byte, d.raw.UnzippedLen)
                    copy(data_stream, d.file.data)
                }
            }

            var output = bytes.Buffer{}
            enc := gob.NewEncoder(&output)
            enc.Encode(d.raw)

            if len(data_stream) > 0 {
                output.Write(data_stream)
            }

            commit_ch <- output
        }(&channel_header)
    }

    /* Do not count "/" as a file, since it is not sent in channel */
    total_files := f.GetFileCount() - 1

    /*
     * Generate the primary filesystem header and write it to the fs_stream
     */
    hdr := rawStreamHeader {
        Signature:  FS_SIGNATURE, /* This signature may be modified in the configuration -- FIXME */
        FileCount:  total_files }

    /* Serializer for fs_header */
    var stream *bytes.Buffer

    if REMOVE_FS_HEADER != true {
        stream = func(hdr rawStreamHeader) *bytes.Buffer {
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
    written, err := f.writeFsStream(f.filename, stream, f.flags)
    if err != nil || int(written) == 0 {
        return util.RetErrStr("Failure in writing raw fs stream")
    }

    return err
}

func loadHeader(data []byte, filename string) (*FSHeader, error) {
    ptr := bytes.NewBuffer(data) /* raw file stream */

    if REMOVE_FS_HEADER != true {
        header, err := func(p *bytes.Buffer) (*rawStreamHeader, error) {
            output := new(rawStreamHeader)

            d := gob.NewDecoder(p)
            if err := d.Decode(output); err != nil {
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
        meta:     make(map[string]*govfsFile),
    }
    output.meta[s("/")] = new(govfsFile)
    output.meta[s("/")].filename = "/"

    /* Enumerate files */
    for {
        if ptr.Len() == 0 {
            break
        }

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

        output.meta[s(file_hdr.Name)] = &govfsFile{
            filename: file_hdr.Name,
            flags: file_hdr.Flags,
            data: nil,
            datasum: "",
        }

        //output.meta[s(file_hdr.Name)].data = make([]byte, decompressed_len)
        if file_hdr.UnzippedLen > 0 {
            output.meta[s(file_hdr.Name)].datasum = file_hdr.RawSum

            var raw_file_data = make([]byte, file_hdr.UnzippedLen)
            ptr.Read(raw_file_data)

            if (file_hdr.Flags & FLAG_COMPRESS_FILES) > 0 {
                var data_ptr *[]byte = &output.meta[s(file_hdr.Name)].data
                *data_ptr = make([]byte, file_hdr.UnzippedLen)

                zipped := bytes.NewBuffer(raw_file_data)
                gzipd, err := gzip.NewReader(zipped)
                if err != nil {
                    gzipd.Close()
                    return nil, err
                }

                gzipd.Close()
                decompressed_len, err := gzipd.Read(*data_ptr)
                if decompressed_len != file_hdr.UnzippedLen || err != nil {
                    return nil, err
                }
                output.t_size += uint(decompressed_len)
            } else {
                output.meta[s(file_hdr.Name)].data = make([]byte, file_hdr.UnzippedLen)
                copy(output.meta[s(file_hdr.Name)].data, raw_file_data)
                output.t_size += uint(file_hdr.UnzippedLen)
            }

            /* Verifiy sums */
            if sum := s(string(output.meta[s(file_hdr.Name)].data)); sum != output.meta[s(file_hdr.Name)].datasum {
                return nil, util.RetErrStr("Invalid file sum")
            }
        }
    }

    return output, nil
}

/*
 * Generate the key used to encrypt/decrypt the raw fs table. The key is composed of the
 *  MD5 sum of the hostname + the FS_SIGNATURE string
 */
func getFsKey() []byte {
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
func readFsStream(name string, flags FlagVal) ([]byte, error) {
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
        key := getFsKey()

        plaintext, err = cryptog.RC4_Decrypt(raw_file, &key)
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
func (f *FSHeader) writeFsStream(name string, data *bytes.Buffer, flags FlagVal) (uint, error) {

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
        key := getFsKey()

        /* Perform RC4 encryption */
        var err error
        ciphertext, err = cryptog.RC4_Encrypt(data.Bytes(), &key)
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

func (f *FSHeader) GetFileCount() uint {
    var total uint = 0
    for range f.meta {
        total += 1
    }

    return total
}

/*
 * Retrieves the file listing in a specific directrory. NOTE: The
 *  `dir` parameter must contain a trailing "/"
 */
func (f *FSHeader) GetFileListDirectory(dir string) ([]string, error) {
	var output []string
	for _, v := range f.meta {
		if strings.Contains(v.filename, dir) {
			output = append(output, v.filename)
		}
	}

	if len(output) == 0 {
		return nil, nil
	}

	return output, nil
}

func (f *FSHeader) GetFileSize(name string) (uint, error) {
    file := f.check(name)
    if file == nil {
        return 0, util.RetErrStr("GetFileSize: File does not exist")
    }

    return uint(len(file.data)), nil
}

func (f *FSHeader) GetTotalFilesizes() uint {
    return f.t_size
}

func (f *FSHeader) GetFileList() []string {
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

