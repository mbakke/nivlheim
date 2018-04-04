package main

import (
	"database/sql"
	"io/ioutil"
	"os"
	"testing"
)

func TestParseFile(t *testing.T) {
	if os.Getenv("NOPOSTGRES") != "" {
		t.Log("No Postgres, skipping test")
		return
	}
	// Create a database connection
	db, err := sql.Open("postgres", "sslmode=disable host=/var/run/postgresql")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Use a temporary tablespace that cleans up after the connection is closed
	_, err = db.Exec("SET search_path TO pg_temp")
	if err != nil {
		t.Fatal(err)
	}
	// It is important that the connection pool only uses this one connection,
	// because if it opens more, they won't have search_path set to pg_temp.
	db.SetMaxOpenConns(1)
	// Run the sql script that creates all the tables
	bytes, err := ioutil.ReadFile("../init.sql")
	if err != nil {
		t.Fatal("Couldn't read init.sql")
	}
	_, err = db.Exec(string(bytes))
	if err != nil {
		t.Fatal(err)
	}
	// Set up some test data
	expectedKernel := "4.15.13-300.fc27.x86_64"
	type file struct {
		filename, content string
	}
	testfiles := []file{
		{
			filename: "/bin/uname -r",
			content:  expectedKernel,
		},
		{
			filename: "/usr/sbin/dmidecode -t system",
			content:  dmiDecodeOutput,
		},
	}
	for _, f := range testfiles {
		_, err = db.Exec("INSERT INTO files(certfp,filename,content,received) "+
			"VALUES('1234',$1,$2,now())", f.filename, f.content)
		if err != nil {
			t.Fatal(err)
		}
	}
	// run the parseFiles Job
	job := parseFilesJob{}
	job.Run(db)

	// verify the results
	var kernel, manufacturer, product, serial sql.NullString
	err = db.QueryRow("SELECT kernel,vendor,model,serialno "+
		"FROM hostinfo WHERE certfp='1234'").
		Scan(&kernel, &manufacturer, &product, &serial)
	if err != nil {
		t.Fatal(err)
	}

	if kernel.String != expectedKernel {
		t.Errorf("Kernel = %s, expected %s", kernel.String, expectedKernel)
	}

	expectedManufacturer := "Dell Inc."
	if manufacturer.String != expectedManufacturer {
		t.Errorf("Manufacturer = %s, expected %s", manufacturer.String, expectedManufacturer)
	}

	expectedProduct := "Latitude E7240"
	if product.String != expectedProduct {
		t.Errorf("Product = %s, expected %s", product.String, expectedProduct)
	}

	expectedSerial := "AFK5678"
	if serial.String != expectedSerial {
		t.Errorf("Serial no = %s, expected %s", serial.String, expectedSerial)
	}

	testOSdetection(db, t)
}

func testOSdetection(db *sql.DB, t *testing.T) {
	type os struct {
		osLabel, filename, content string
	}
	tests := []os{
		{
			osLabel:  "RHEL 7",
			filename: "/etc/redhat-release",
			content:  "Red Hat Enterprise Linux Workstation release 7.4 (Maipo)",
		},
		{
			osLabel:  "Fedora 27",
			filename: "/etc/redhat-release",
			content:  "Fedora release 27 (Twenty Seven)",
		},
	}
	const fileID = 10000
	const certfp = "AA11"
	for _, test := range tests {
		_, err := db.Exec("INSERT INTO files(fileid,certfp,filename,content,received) "+
			"VALUES($1,$2,$3,$4,now())", fileID, certfp, test.filename, test.content)
		if err != nil {
			t.Fatal(err)
		}
		db.Exec("UPDATE hostinfo SET os=null, os_edition=null WHERE certfp=$1",
			certfp)
		parseFile(db, fileID)
		db.Exec("DELETE FROM files WHERE fileid=$1", fileID)
		var os, osEdition sql.NullString
		err = db.QueryRow("SELECT os,os_edition FROM hostinfo WHERE certfp=$1",
			certfp).Scan(&os, &osEdition)
		if err != nil {
			t.Fatal(err)
		}
		if os.String != test.osLabel {
			t.Errorf("OS is %s, expected %s", os.String, test.osLabel)
		}
	}
}

const dmiDecodeOutput = `# dmidecode 3.1
Getting SMBIOS data from sysfs.
SMBIOS 2.7 present.

Handle 0x0001, DMI type 1, 27 bytes
System Information
	Manufacturer: Dell Inc.
	Product Name: Latitude E7240
	Version: 01
	Serial Number: AFK5678
	UUID: 4C4C4544-0054-4B10-804E-CAC04F565931
	Wake-up Type: Power Switch
	SKU Number: Latitude E7240
	Family: Not Specified

Handle 0x0024, DMI type 12, 5 bytes
System Configuration Options
	Option 1: To Be Filled By O.E.M.

Handle 0x0025, DMI type 15, 35 bytes
System Event Log
	Area Length: 4 bytes
	Header Start Offset: 0x0000
	Header Length: 2 bytes
	Data Start Offset: 0x0002
	Access Method: Indexed I/O, one 16-bit index port, one 8-bit data port
	Access Address: Index 0x046A, Data 0x046C
	Status: Invalid, Not Full
	Change Token: 0x00000000
	Header Format: No Header
	Supported Log Type Descriptors: 6
	Descriptor 1: End of log
	Data Format 1: OEM-specific
	Descriptor 2: End of log
	Data Format 2: OEM-specific
	Descriptor 3: End of log
	Data Format 3: OEM-specific
	Descriptor 4: End of log
	Data Format 4: OEM-specific
	Descriptor 5: End of log
	Data Format 5: OEM-specific
	Descriptor 6: End of log
	Data Format 6: OEM-specific

Handle 0x002D, DMI type 32, 20 bytes
System Boot Information
	Status: No errors detected
`
