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
)

func TestIOSanity(t *testing.T) {
	out("[+] Running Standard I/O Sanity Test...")
	
    header := create_db("<path>")
    if header == nil {
        t.Errorf("error: Failed to obtain header")
    }
	out("[+] Test 1 PASS")
    
    // The root file "/" must at least exist
    if file := header.create("/"); file == nil || file.filename != "/" {
        t.Errorf("error: Failed to return root handle")
    }
	out("[+] Test 2 PASS")
    
    /*
     * Try to delete the root file "/"
     */
    if header.delete("/") == 0 {
        t.Errorf("error: Cannot delete root -- critical")
    }
	out("[+] Test 3 PASS")

    /*
     * Attempt to write to a nonexistant file
     */
	var data = []byte{ 1, 2 }
    if header.write("/folder5/folder5/file5", data) != STATUS_ERROR {
        t.Errorf("error: Cannot write to a nonexistant file")
    }
	out("[+] Test 4 PASS")
	
    /*
     * Attempt to create a new file0
     */
    if file := header.create("/folder0/folder0/file0"); file == nil {
        t.Errorf("error: file0 cannot be created")
    }
	out("[+] Test 5.0 PASS")
	
    /*
     * Attempt to create a new file0
     */
    if file := header.create("/folder0/folder0/file0"); file != nil {
        t.Errorf("error: file0 cannot be created twice")
    }
	out("[+] Test 5.1 PASS")
    
    /*
     * Write some data into file0
     */
    data = []byte{ 1, 2, 3, 4 }
    if header.write("/folder0/folder0/file0", data) != 0 {
        t.Errorf("error: Failed to write data in file0")
    }
	out("[+] Test 6 PASS")
    
    /*
     * Attempt to create a new file3
     */
    if file := header.create("/folder1/folder0/file3"); file == nil {
        t.Errorf("error: file3 cannot be created")
    }
	out("[+] Test 7 PASS")
    
    /*
     * Write some data into file3
     */
    var data2 = []byte{ 1, 2, 3, 4, 5, 6, 7 }
    if header.write("/folder1/folder0/file3", data2) != 0 {
        t.Errorf("error: Failed to write data in file3")
    }
	out("[+] Test 8 PASS")
    
    /*
     * Read the written data from file0 and compare
     */
    output_data := header.read("/folder0/folder0/file0")
    if output_data == nil || len(output_data) != len(data) || header.t_size - 7 /* len(file3) */ != uint(len(data)) {
        t.Errorf("error: Failed to read data from file0")
    }
	out("[+] Test 9 PASS")
    
    /*
     * Read the written data from file3 and compare
     */
    output_data = header.read("/folder1/folder0/file3")
    if output_data == nil || len(output_data) != len(data2) || header.t_size - 4 /* len(file0) */ != uint(len(data2)) {
        t.Errorf("error: Failed to read data from file3")
    }
	out("[+] Test 10 PASS")
    
    /*
     * Write other data to file0
     */
    data = []byte{ 1, 2, 3 }
    if header.write("/folder0/folder0/file0", data) != 0 {
        t.Errorf("error: Failed to write data in file1")
    }   
	out("[+] Test 11 PASS")
    
    /*
     * Read the new data from file0
     */
    output_data = header.read("/folder0/folder0/file0")
    if output_data == nil || len(output_data) != len(data) {
        t.Errorf("error: Failed to read data from file1")
    }
	out("[+] Test 12 PASS")
	
    /*
     * Attempt to create a new file5. This will be a blank file
     */
    if file := header.create("/folder2/file5"); file == nil {
        t.Errorf("error: file3 cannot be created")
    }
	out("[+] Test 13 PASS")
    
    /*
     * Delete file0 -- complete this
     */
	 
	/*
	 * Create just a folder
	 */
    if file := header.create("/folder2/file5/"); file == nil {
        t.Errorf("error: folder file5 cannot be created")
    }
	out("[+] Test 15 PASS")	

     /*
      * Unmount/commit database to file
      */
    if header.unmount_db(nil) != 0 {
        t.Errorf("error: Failed to commit database")
    }
	out("[+] Test 16 PASS")
     
    
    time.Sleep(10000)
}
