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

import (
    "testing"
    "time"
    "os"
)

const FS_DATABASE_FILE string = "test_db"

func TestIOSanity(t *testing.T) {
    out("[+] Running Standard I/O Sanity Test...")

    /* Remove the test database if it exists */
    if _, err := os.Stat(FS_DATABASE_FILE); os.IsExist(err) {
        os.Remove(FS_DATABASE_FILE)
    }
    var header = create_db(FS_DATABASE_FILE)
    if header == nil {
        drive_fail("TEST1: Failed to obtain header", t)
    }
    out("[+] Test 1 PASS")
    
    // The root file "/" must at least exist
    if file, err := header.create("/"); file != nil && err != STATUS_ERROR {
        drive_fail("TEST2: Failed to return root handle", t)
    }
    out("[+] Test 2 PASS")
    
    /*
     * Try to delete the root file "/"
     */
    if header.delete("/") == 0 {
        drive_fail("TEST3: Cannot delete root -- critical", t)
    }
    out("[+] Test 3 PASS")

    /*
     * Attempt to write to a nonexistant file
     */
    var data = []byte{ 1, 2 }
    if header.write("/folder5/folder5/file5", data) != STATUS_ERROR {
        drive_fail("TEST4: Cannot write to a nonexistant file", t)
    }
    out("[+] Test 4 PASS")

    /*
     * Create empty file file9
     */
    if file, err := header.create("/folder5/folder4/folder2/file9"); file == nil || err == STATUS_ERROR {
        drive_fail("TEST4.1: file9 cannot be created", t)
    }
    out("[+] Test 4.1 PASS")

    /*
     * Attempt to create a new file0
     */
    if file, err := header.create("/folder0/folder0/file0"); file == nil || err == STATUS_ERROR {
        drive_fail("TEST5.0: file0 cannot be created", t)
    }
    out("[+] Test 5.0 PASS")

    /*
     * Attempt to create a new file0, this will fail since it should already exist
     */
    if file, err := header.create("/folder0/folder0/file0"); file != nil && err != STATUS_EXISTS {
        drive_fail("TEST5.1: file0 cannot be created twice", t)
    }
    out("[+] Test 5.1 PASS")

    
    /*
     * Write some data into file0
     */
    data = []byte{ 1, 2, 3, 4 }
    if header.write("/folder0/folder0/file0", data) != STATUS_OK {
        drive_fail("TEST6: Failed to write data in file0", t)
    }
    out("[+] Test 6 PASS")

    /*
     * Check that the size of file0 is 4
     */
    if k, _ := header.get_file_size("/folder0/folder0/file0"); k != uint(len(data)) {
        drive_fail("TEST6.1: The size of data does not match", t)
    }
    out("[+] Test 6.1 PASS")
    
    /*
     * Attempt to create a new file3
     */
    if file, err := header.create("/folder1/folder0/file3"); file == nil || err == STATUS_ERROR {
        drive_fail("TEST7: file3 cannot be created", t)
    }
    out("[+] Test 7 PASS")
    
    /*
     * Write some data into file3
     */
    var data2 = []byte{ 1, 2, 3, 4, 5, 6, 7 }
    if header.write("/folder1/folder0/file3", data2) != 0 {
        drive_fail("TEST8: Failed to write data in file3", t)
    }
    out("[+] Test 8 PASS")

    /*
     * Write some data into file3
     */
    if header.write("/folder1/folder0/file3", data2) != 0 {
        drive_fail("TEST8.1: Failed to write data in file3", t)
    }
    out("[+] Test 8.1 PASS")
    
    /*
     * Read the written data from file0 and compare
     */
    output_data, err := header.read("/folder0/folder0/file0")
    err += 1
    if output_data == nil || len(output_data) != len(data) || header.t_size - 7 /* len(file3) */ != uint(len(data)) {
        drive_fail("TEST9: Failed to read data from file0", t)
    }
    out("[+] Test 9 PASS")
    
    /*
     * Read the written data from file3 and compare
     */
    output_data, err = header.read("/folder1/folder0/file3")
    if output_data == nil || len(output_data) != len(data2) || header.t_size - 4 /* len(file0) */ != uint(len(data2)) {
        drive_fail("TEST10: Failed to read data from file3", t)
    }
    out("[+] Test 10 PASS")
    
    /*
     * Write other data to file0
     */
    data = []byte{ 1, 2, 3 }
    if header.write("/folder0/folder0/file0", data) != 0 {
        drive_fail("TEST11: Failed to write data in file1", t)
    }   
    out("[+] Test 11 PASS")
    
    /*
     * Read the new data from file0
     */
    output_data, err = header.read("/folder0/folder0/file0")
    if output_data == nil || len(output_data) != len(data) {
        drive_fail("TEST12: Failed to read data from file1", t)
    }
    out("[+] Test 12 PASS")

    /*
     * Attempt to create a new file5. This will be a blank file
     */
    if file, err := header.create("/folder2/file7"); file == nil || err == STATUS_ERROR {
        drive_fail("TEST13: file3 cannot be created", t)
    }
    out("[+] Test 13 PASS")
    
    /*
     * Delete file0 -- complete this
     */
    // FIXME/ADDME

    /*
     * Create just a folder
     */
    if file, err := header.create("/folder2/file5/"); file == nil || err == STATUS_ERROR {
        drive_fail("TEST15: folder file5 cannot be created", t)
    }
    out("[+] Test 15 PASS")

     /*
      * Unmount/commit database to file
      */
    if header.unmount_db() != 0 {
        drive_fail("TEST16: Failed to commit database", t)
    }
    out("[+] Test 16 PASS")

    /*
     * Print out files
     */
    file_list := header.get_file_list()
    for _, e := range file_list {
        out(e)
    }
    
    time.Sleep(10000)
}

func drive_fail(output string, t *testing.T) {
    t.Errorf(output)
    t.FailNow()
}
