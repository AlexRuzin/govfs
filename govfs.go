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
    "time"
    "compress/gzip"
    "bytes"
)

const STATUS_ERROR      int = -1
const STATUS_OK         int = 0

const IRP_PURGE         int = 2 /* Flush the entire database and all files */
const IRP_DELETE        int = 3 /* Delete a file/folder */
const IRP_WRITE         int = 4 /* Write data to a file */
const IRP_CREATE        int = 5 /* Create a new file or folder */

const FLAG_FILE			int = 1
const FLAG_DIRECTORY	int = 2

type gofs_header struct {
    filename    string
    key         [16]byte
    meta        map[string]*gofs_file
    t_size      uint /* Total size of all files */
    io_in       chan *gofs_io_block
}

type gofs_file struct {
    fsheader    *gofs_header
    filename    string
    filetype    int /* FLAG_FILE, FLAG_DIRECTORY */
    datasum     string
    data        []byte
    io_out      chan *gofs_io_block
}

type gofs_io_block struct {
    file        *gofs_file
    name        string
    data        []byte
    status      int /* 0 == fail, 1 == ok, 2 == purge, 3 == delete, 4 == write */
    flags       int
    create_io   chan *gofs_io_block /* Used only for IRP_CREATE, since no gofs_file exists yet */
}

func create_db(filename string) *gofs_header {
    header                          := new(gofs_header)
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
                io.status = STATUS_ERROR
                if io.file.filename == "/" { /* Cannot delete the root file */
                    io.status = STATUS_ERROR
                    io.file.io_out <- io
                } else {
                    if i := f.check(io.name); i != nil {
                        delete(f.meta, s(io.name))
                        f.meta[s(io.name)] = nil
                        io.status = STATUS_OK
                    }
                    io.file.io_out <- io
                }
            case IRP_WRITE:
                /* WRITE */
                if i := f.check(io.name); i != nil {
                    if f.write_internal(i, io.data) != len(io.data) {
						io.status = STATUS_OK
						io.file.io_out <- io
                    } else {
                        io.status = STATUS_ERROR
                        io.file.io_out <- io
                    }
                }
                /* File doesn't exist */
                io.status = STATUS_ERROR
                io.file.io_out <- io
            case IRP_CREATE: 
				if f.check(io.name) != nil {
					io.status = STATUS_ERROR
					io.file.io_out <- io					
				}
			
				f.meta[s(io.name)] = new(gofs_file)
				io.file = f.meta[s(io.name)]				
				io.file.filename = io.name
				
				if string(io.name[len(io.name) - 1:]) == "/" {
					io.file.filetype = FLAG_DIRECTORY
				} else {
					io.file.filetype = FLAG_FILE
				}
                
                /* Recursively create all subdirectory files */
				/* FIXME/ADDME */
				io.status = STATUS_OK
				io.create_io <- io
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
        out("last: " + header.file.filename)
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

func (f *gofs_header) wait_for_irp_channel(file *gofs_file) chan *gofs_io_block {
    for file.io_out != nil {
        time.Sleep(10)
    }
    file.io_out = make(chan *gofs_io_block)

    return file.io_out
}

func (f *gofs_header) generate_irp(name string, data []byte, irp_type int) *gofs_io_block {
    switch irp_type {
    case IRP_DELETE:
        /* DELETE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil /* ERROR -- deleting non-existant file */
        }

        file_header.io_out = f.wait_for_irp_channel(file_header)

        irp := new(gofs_io_block)
        irp.file = file_header
        irp.name = name

        irp.status = IRP_DELETE

        return irp
    case IRP_WRITE:
        /* WRITE */
        var file_header = f.check(name)
        if file_header == nil {
            return nil
        }

        file_header.io_out = f.wait_for_irp_channel(file_header)

        irp := new(gofs_io_block)
        irp.file = file_header
        irp.name = name
        irp.data = make([]byte, len(data))
        copy(irp.data, data)

        irp.status = IRP_WRITE /* write IRP request */

        return irp
        
    case IRP_CREATE:
        /* CREATE IRP */
        irp := new(gofs_io_block)
        irp.name = name
        irp.status = IRP_CREATE
        irp.create_io = make(chan *gofs_io_block)
        
        return irp
    }

	
	
    return nil
}

func (f *gofs_header) create(name string) *gofs_file {
    file := f.check(name)
    if file != nil {
        return file
    }   
    var irp *gofs_io_block = f.generate_irp(name, nil, IRP_CREATE)
    
	out("testt3")
	if f.io_in == nil {
		out("FAIL")
	}
    f.io_in <- irp
	out("d:" + irp.name)
    output_irp := <- irp.create_io
    if output_irp.file == nil {
        return nil
    }
	close(output_irp.create_io)

    return output_irp.file
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
    var output_irp = <- irp.file.io_out

    close(irp.file.io_out)
    irp.file.io_out = nil
    if output_irp.status != STATUS_OK {
        return STATUS_ERROR /* failed */
    }

    return STATUS_OK
}

func (f *gofs_header) write(name string, d []byte) int {
    irp := f.generate_irp(name, d, IRP_WRITE)
    if irp == nil {
        return STATUS_ERROR /* FAILURE */
    }

    /*
     * Send the write request IRP and receive the response
     *  IRP indicating the write status of the request
     */
    f.io_in <- irp
    var output_irp = <- irp.file.io_out

    close(irp.file.io_out)
    irp.file.io_out = nil
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

    return len(d.data)
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
