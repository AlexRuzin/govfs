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

package gofs

// TODO
// create() can either create a folder or a file. 
// When a folder/file is created, make all subdirectories in the map as well

/* TEST5
 * Supports:
 *  [+] UTF=8 file names <- not yet
 *  [+] 2^128 files
 *  [+] o(1) seek/write time for metadata
 *
 */

import (
    "fmt"
    "crypto/md5"
    "encoding/hex"
    "compress/gzip"
    "bytes"
	"sync"
    "strings"
)

const STATUS_ERROR      int = -1
const STATUS_EXISTS		int = -2
const STATUS_OK         int = 0

const IRP_PURGE         int = 2 /* Flush the entire database and all files */
const IRP_DELETE        int = 3 /* Delete a file/folder */
const IRP_WRITE         int = 4 /* Write data to a file */
const IRP_CREATE        int = 5 /* Create a new file or folder */

const FLAG_FILE         int = 1
const FLAG_DIRECTORY    int = 2

type gofs_header struct {
    filename    string
    key         [16]byte
    meta        map[string]*gofs_file
    t_size      uint /* Total size of all files */
    io_in       chan *gofs_io_block
	create_sync	sync.Mutex
}

type gofs_file struct {
    filename    string
    filetype    int /* FLAG_FILE, FLAG_DIRECTORY */
    datasum     string
    data        []byte
	lock		sync.Mutex
}

type gofs_io_block struct {
    file        *gofs_file
    name        string
    data        []byte
    status      int /* 0 == fail, 1 == ok, 2 == purge, 3 == delete, 4 == write */
    flags       int
    io_out      chan *gofs_io_block
}

func create_db(filename string) *gofs_header {
    var header                      = new(gofs_header)
    header.filename                 = filename
    header.meta                     = make(map[string]*gofs_file)
    header.meta[s("/")]             = new(gofs_file)
    header.meta[s("/")].filename    = "/"

    /* i/o channel processor. Performs i/o to the filesystem */
    header.io_in = make(chan *gofs_io_block)
    go func (f *gofs_header) {
        for {
            var io = <- header.io_in
            
            switch io.status {
            case IRP_PURGE:
                /* PURGE */
                out("ERROR: PURGING")
                close(header.io_in)
                return
            case IRP_DELETE:
                /* DELETE */
                // FIXME/ADDME
                io.status = STATUS_ERROR
                if io.file.filename == "/" { /* Cannot delete the root file */
                    io.status = STATUS_ERROR
                    io.io_out <- io
                } else {
                    if i := f.check(io.name); i != nil {
                        delete(f.meta, s(io.name))
                        f.meta[s(io.name)] = nil
                        io.status = STATUS_OK
                    }
                    io.io_out <- io
                }
            case IRP_WRITE:
                /* WRITE */
                if i := f.check(io.name); i != nil {
					io.file.lock.Lock()
                    if f.write_internal(i, io.data) == len(io.data) {
                        io.status = STATUS_OK
						io.file.lock.Unlock()
                        io.io_out <- io
                    } else {
                        io.status = STATUS_ERROR
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
                        if t := f.check(sub_directory); t != nil {
                            return
                        }

                        f.meta[s(tmp)] = new (gofs_file)
                        f.meta[s(tmp)].filename = sub_directory
                        f.meta[s(tmp)].filetype = FLAG_DIRECTORY
                    } (tmp, f)
                }

                io.status = STATUS_OK
                io.io_out <- io
            }
        }
    } (header)

    return header
}

func (f *gofs_header) unmount_db(filename *string) int {
    type comp_data struct {
        file *gofs_file
        data_compressed bytes.Buffer
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
                w := gzip.NewWriter(&d.data_compressed)
                w.Write(d.file.data)
                w.Close()
            }
            commit_ch <- d            
        }(header)
    }
    
    /* Do not count "/" as a file, since it is not sent in channel */
    total_files := f.get_file_count() - 1
    for total_files != 0 {
        var header = <- commit_ch
		out("inbound: " + header.file.filename)
        total_files -= 1
    }

    close(commit_ch)
    return STATUS_OK
}

func (f *gofs_header) get_file_count() uint {
    var total uint = 0
    for range f.meta {
        total += 1
    }
    
    return total
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

        irp.status = IRP_DELETE

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

        irp.status = IRP_WRITE /* write IRP request */

        return irp
        
    case IRP_CREATE:
        /* CREATE IRP */
        irp := new(gofs_io_block)
        irp.name = name
        irp.status = IRP_CREATE
        irp.io_out = make(chan *gofs_io_block)
        
        return irp
    }    
    
    return nil
}

func (f *gofs_header) create(name string) (*gofs_file, int) {
    if file := f.check(name); file != nil {
        return nil, STATUS_EXISTS
    }  
	f.create_sync.Lock()
    var irp *gofs_io_block = f.generate_irp(name, nil, IRP_CREATE)
    
    f.io_in <- irp
    output_irp := <- irp.io_out
	f.create_sync.Unlock()
    if output_irp.file == nil {
        return nil, STATUS_ERROR
    }
    close(output_irp.io_out)

    return output_irp.file, STATUS_OK
}

func (f *gofs_header) read(name string) []byte {
    var file_header = f.check(name)
    if file_header == nil {
        return nil
    }

    output := make([]byte, len(file_header.data))
    copy(output, file_header.data)
    return output
}

func (f *gofs_header) delete(name string) int {
    irp := f.generate_irp(name, nil, IRP_DELETE)
    if irp == nil {
        return STATUS_ERROR /* ERROR -- File does not exist */
    }

    f.io_in <- irp
    var output_irp = <- irp.io_out

    close(irp.io_out)
    if output_irp.status != STATUS_OK {
        return STATUS_ERROR /* failed */
    }

    return STATUS_OK
}

func (f *gofs_header) write(name string, d []byte) int {
	if i := f.check(name); i == nil {
		return STATUS_ERROR
	}
	
    irp := f.generate_irp(name, d, IRP_WRITE)
    if irp == nil {
        return STATUS_ERROR /* FAILURE */
    }

    /*
     * Send the write request IRP and receive the response
     *  IRP indicating the write status of the request
     */
    f.io_in <- irp
    var output_irp = <- irp.io_out

    close(irp.io_out)
    if output_irp.status != STATUS_OK {
        return STATUS_ERROR /* failed */
    }

    return STATUS_OK
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

func (f *gofs_header) get_total_filesizes() uint {
    return f.t_size
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
