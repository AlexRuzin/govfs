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

import (
    "testing"
    "time"
    "os"
    "io"
    "bytes"
    "runtime"
    "github.com/AlexRuzin/util"
    "strconv"
)

const FS_DATABASE_FILE string = "test_db"


func TestFSWriter(t *testing.T) {
    /*
     * This test will generate the raw fs stream file, along with some contents
     *  that will be later loaded by the TestFSReader() method
     */
    util.DebugOut("[+] Running Standard I/O Sanity Test...")

    /* Remove the test database if it exists */
    var filename = gen_raw_filename(FS_DATABASE_FILE)
    if _, err := os.Stat(filename); os.IsExist(err) {
        os.Remove(filename)
    }

    var header, err = CreateDatabase(filename, FLAG_DB_CREATE)
    if header == nil || err != nil {
        drive_fail(err.Error(), t)
    }
    if err := header.StartIOController(); err != nil {
        drive_fail("TEST1.1: Failed to start IOController", t)
    }
    util.DebugOut("[+] Test 1 PASS")
    
    // The root file "/" must at least exist
    if file, err := header.Create("/"); file != nil && err == nil {
        drive_fail("TEST2: Failed to return root handle", t)
    }
    util.DebugOut("[+] Test 2 PASS")
    
    /*
     * Try to delete the root file "/"
     */
    if header.Delete("/") == nil {
        drive_fail("TEST3: Cannot delete root -- critical", t)
    }
    util.DebugOut("[+] Test 3 PASS")

    /*
     * Attempt to write to a nonexistant file
     */
    var data = []byte{ 1, 2 }
    if header.Write("/folder5/folder5/file5", data) == nil {
        drive_fail("TEST4: Cannot write to a nonexistant file", t)
    }
    util.DebugOut("[+] Test 4 PASS")

    /*
     * Create empty file file9
     */
    if file, err := header.Create("/folder5/folder4/folder2/file9"); file == nil || err != nil {
        drive_fail("TEST4.1: file9 cannot be created", t)
    }
    util.DebugOut("[+] Test 4.1 PASS")

    /*
     * Attempt to create a new file0
     */
    if file, err := header.Create("/folder0/folder0/file0"); file == nil || err != nil {
        drive_fail("TEST5.0: file0 cannot be created", t)
    }
    util.DebugOut("[+] Test 5.0 PASS")

    /*
     * Attempt to create a new file0, this will fail since it should already exist
     */
    if file, err := header.Create("/folder0/folder0/file0"); file != nil && err == nil {
        drive_fail("TEST5.1: file0 cannot be created twice", t)
    }
    util.DebugOut("[+] Test 5.1 PASS")

    
    /*
     * Write some data into file0
     */
    data = []byte{ 1, 2, 3, 4 }
    if header.Write("/folder0/folder0/file0", data) != nil {
        drive_fail("TEST6: Failed to write data in file0", t)
    }
    util.DebugOut("[+] Test 6 PASS")

    /*
     * Check that the size of file0 is 4
     */
    if k, _ := header.GetFileSize("/folder0/folder0/file0"); k != uint(len(data)) {
        drive_fail("TEST6.1: The size of data does not match", t)
    }
    util.DebugOut("[+] Test 6.1 PASS")
    
    /*
     * Attempt to create a new file3
     */
    if file, err := header.Create("/folder1/folder0/file3"); file == nil || err != nil {
        drive_fail("TEST7: file3 cannot be created", t)
    }
    util.DebugOut("[+] Test 7 PASS")
    
    /*
     * Write some data into file3
     */
    var data2 = []byte{ 1, 2, 3, 4, 5, 6, 7 }
    if header.Write("/folder1/folder0/file3", data2) != nil {
        drive_fail("TEST8: Failed to write data in file3", t)
    }
    util.DebugOut("[+] Test 8 PASS")

    /*
     * Write some data into file3
     */
    if header.Write("/folder1/folder0/file3", data2) != nil {
        drive_fail("TEST8.1: Failed to write data in file3", t)
    }
    util.DebugOut("[+] Test 8.1 PASS")
    
    /*
     * Read the written data from file0 and compare
     */
    output_data, _ := header.Read("/folder0/folder0/file0")
    if output_data == nil || len(output_data) != len(data) || header.t_size - 7 /* len(file3) */ != uint(len(data)) {
        drive_fail("TEST9: Failed to read data from file0", t)
    }
    util.DebugOut("[+] Test 9 PASS")
    
    /*
     * Read the written data from file3 and compare
     */
    output_data, _ = header.Read("/folder1/folder0/file3")
    if output_data == nil || len(output_data) != len(data2) || header.t_size - 4 /* len(file0) */ != uint(len(data2)) {
        drive_fail("TEST10: Failed to read data from file3", t)
    }
    util.DebugOut("[+] Test 10 PASS")

    /* Test GetFileListDirectory */
    fileEnum, err := header.GetFileListDirectory("/folder1/")
    if err != nil {
        drive_fail("TEST10.1: Failed to retrieve file listing", t)
    }
    for _, v := range fileEnum {
        util.DebugOut(v)
    }

    /*
     * Write other data to file0
     */
    data = []byte{ 1, 2, 3 }
    if header.Write("/folder0/folder0/file0", data) != nil {
        drive_fail("TEST11: Failed to write data in file1", t)
    }   
    util.DebugOut("[+] Test 11 PASS")
    
    /*
     * Read the new data from file0
     */
    output_data, _ = header.Read("/folder0/folder0/file0")
    if output_data == nil || len(output_data) != len(data) {
        drive_fail("TEST12: Failed to read data from file1", t)
    }
    util.DebugOut("[+] Test 12 PASS")

    /*
     * Attempt to create a new file5. This will be a blank file
     */
    if file, err := header.Create("/folder2/file7"); file == nil || err != nil {
        drive_fail("TEST13: file3 cannot be created", t)
    }
    util.DebugOut("[+] Test 13 PASS")
    
    /*
     * Delete file0 -- complete this
     */
    // FIXME/ADDME

    /*
     * Create just a folder
     */
    if file, err := header.Create("/folder2/file5/"); file == nil || err != nil {
        drive_fail("TEST15: folder file5 cannot be created", t)
    }
    util.DebugOut("[+] Test 15 PASS")

    /*
     * Tests the Reader interface
     */
    reader, err := header.NewReader("/folder0/folder0/file0")
    if err != nil {
        drive_fail("TEST15.1: Failed to create Reader", t)
    }

    file0data := make([]byte, 3)
    data_read, err := reader.Read(file0data)
    if data_read != len(data) || err != io.EOF || bytes.Compare(file0data, data) != 0 {
        drive_fail("TEST15.2: Failed to read from NewReader", t)
    }

    /* Test the reader interface again */
    file0data = make([]byte, 1)
    data_read, err = reader.Read(file0data)
    if data_read != 1 || err != nil || file0data[0] != 1 {
        drive_fail("TEST15.3: Invalid Reader interface behaviour", t)
    }
    util.DebugOut("[+] Test 15.1, 15.2, 15.3 PASS -- Reader interface")

    /*
     * Tests the Writer interface
     */
    file0data = []byte{1, 2, 3, 4, 5, 6, 7, 8}
    writer, err := header.NewWriter("/folder0/folder0/file0")
    if writer == nil || err != nil {
        drive_fail("TEST15.4: Invalid Writer object", t)
    }

    written, err := writer.Write(file0data)
    if written != len(file0data) || err != io.EOF {
        drive_fail("TEST15.4: Invalid Writer response", t)
    }

    file0data = make([]byte, 8)
    data_read, err = reader.Read(file0data)
    if data_read != 8 || err != io.EOF || file0data[0] != 1 || file0data[1] != 2 {
        drive_fail("TEST15.5: Invalid Reader data",t )
    }
    util.DebugOut("[+] Test 15.4, 15.5 PASS -- Writer interface")

    /*
     * Print out files
     */
    file_list := header.GetFileList()
    for _, e := range file_list {
        util.DebugOut(e)
    }

    /*
     * Unmount/commit database to file
     */
    if err := header.UnmountDB(0 /*FLAG_COMPRESS_FILES*/); err != nil {
        drive_fail("TEST16: Failed to commit database", t)
    }
    util.DebugOut("[+] Test 16 PASS. Raw FS stream written to: " + header.filename)
    util.DebugOut("Total File Content Size: " + string(header.GetTotalFilesizes()))

    time.Sleep(10000)
}

func TestFSReader(t *testing.T) {
    /*
     * Read in FS_DATABASE_FILE and do basic tests
     */
    var filename = gen_raw_filename(FS_DATABASE_FILE)
    util.DebugOut("[+] Loading Raw FS stream file: " + filename)

    /* Remove the test database if it exists */
    if _, err := os.Stat(filename); os.IsNotExist(err) {
        drive_fail("error: Standard raw fs stream " + filename + " does not exist", t)
    }

    header, err := CreateDatabase(filename, FLAG_DB_LOAD)
    if header == nil || err != nil {
        drive_fail("TEST1: Failed to obtain header", t)
    }

    if err := header.StartIOController(); err != nil {
        drive_fail("TEST1.1: Failed to start IOController", t)
    }
    util.DebugOut("[+] Test 1 PASS (Loaded FS stream)")

    /*
     * Tests the Writer interface
     */
    if file, err := header.Create("/NewestFolder/Subdirectory/GoodBoy.bin"); file == nil || err != nil {
        drive_fail("TEST2.1: folder file5 cannot be created", t)
    }

    file0data := []byte("whattttttt")
    writer, err := header.NewWriter("/NewestFolder/Subdirectory/GoodBoy.bin")
    if writer == nil || err != nil {
        drive_fail("TEST2.2: Invalid Writer object", t)
    }

    written, err := writer.Write(file0data)
    if written != len(file0data) || err != io.EOF {
        drive_fail("TEST2.3: Invalid Writer response", t)
    }
    util.DebugOut("[+] Test 2 PASS")

    /*
     * Print out files
     */
    file_list := header.GetFileList()
    for _, e := range file_list {
        util.DebugOut(e)
    }

    util.DebugOut("Total File Content Size: " + strconv.Itoa(int(header.GetTotalFilesizes())))
}

func gen_raw_filename(suffix string) string {
    if runtime.GOOS == "windows" {
        return os.Getenv("TEMP") + "\\" + suffix + ".db"
    }

    if runtime.GOOS == "linux" {
        return suffix + ".db"
    }

    return suffix + ".db"
}

func drive_fail(output string, t *testing.T) {
    t.Errorf(output)
    t.FailNow()
}
