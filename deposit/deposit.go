// Copyright 2011 Ken Friis Larsen. All rights reserved.
package deposit

import (
	"archive/zip"
        "bytes"
        "crypto/sha1"
        "fmt"
        "http"
        "io"
        "os"
        "template"
        "time"
//	"strconv"
	"strings"
)

// These imports were added for deployment on App Engine.
import (
        "appengine"
        "appengine/datastore"
        "appengine/user"
)

var (
        uploadTemplate = template.Must(template.ParseFile("apupload.html"))
        viewTemplate   = template.Must(template.ParseFile("singleupload.html"))
        adminTemplate  = template.Must(template.ParseFile("admin.html"))
        errorTemplate  = template.Must(template.ParseFile("error.html"))
)

const viewPath = "/upload/"

func init() {
        http.HandleFunc("/", errorHandler(upload))
        http.HandleFunc("/admin/", errorHandler(admin))
        http.HandleFunc(viewPath, errorHandler(showupload))
}

// Upload is the type used to hold the uploaded data in the datastore.
type Upload struct {
        Name      string
        KUemail   string
        Comments  string
        Timestamp datastore.Time
        PdfFile   []byte
        SrcZip    []byte
}

// upload is the HTTP handler for uploading deposits; it handles "/".
func upload(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" {
                // No upload; show the upload form.
                uploadTemplate.Execute(w, nil)
                return
        }

        pdf, _, err := r.FormFile("pdffile")
        check(err)
        defer pdf.Close()

        zip, _, err := r.FormFile("zipfile")
        check(err)
        defer zip.Close()


        // Grab the pdf data
        var pdfbuf bytes.Buffer
        io.Copy(&pdfbuf, pdf)

        // Grab the zip data
        var zipbuf bytes.Buffer
        io.Copy(&zipbuf, zip)

        up := Upload{
        Name: r.FormValue("name"),
        KUemail: r.FormValue("kuemail"),
        Comments: r.FormValue("comments"),
        Timestamp: datastore.SecondsToTime(time.Seconds()),
        PdfFile: pdfbuf.Bytes(),
        SrcZip: zipbuf.Bytes(),
        }


        // Create an App Engine context for the client's request.
        c := appengine.NewContext(r)

        // Save the upload under a (hopefully) unique key, a hash of
        // the data
        key := datastore.NewKey(c, "Upload", keyOf(&up), 0, nil)
        _, err = datastore.Put(c, key, &up)
        check(err)

        // Redirect to /upload/ using the key.
        http.Redirect(w, r, viewPath+key.StringID(), http.StatusFound)
}

// keyOf returns (part of) the SHA-1 hash of the data, as a hex string.
func keyOf(up *Upload) string {
        sha := sha1.New()
        io.WriteString(sha, up.Name)
        io.WriteString(sha, up.KUemail)
        io.WriteString(sha, up.Comments)
        //sha.Write(up.Timestamp)
        sha.Write(up.PdfFile)
        sha.Write(up.SrcZip)
        return fmt.Sprintf("%x", string(sha.Sum())[0:10])
}

// showupload is the HTTP handler for a single upload; it handles "/upload/".
func showupload(w http.ResponseWriter, r *http.Request) {
        keystring := r.URL.Path[len(viewPath):]
        c := appengine.NewContext(r)
        key := datastore.NewKey(c, "Upload", keystring, 0, nil)
        up := new(Upload)
        err := datastore.Get(c, key, up)
        check(err)

        m := map[string]interface{}{
		"Name": up.Name, 
		"Time": up.Timestamp.Time().Format(time.RFC850), 
		"Key": keystring,
	}

        err = viewTemplate.Execute(w, m);
        check(err)
}

func admin(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	u := user.Current(c)
	if u == nil {
		url, err := user.LoginURL(c, r.URL.String())
		check(err)
		w.Header().Set("Location", url)
		w.WriteHeader(http.StatusFound)
		return
	}
        pathstring := r.URL.Path[len("/admin/"):]
	q := datastore.NewQuery("Upload").Order("Timestamp").Order("KUemail")

	if strings.HasPrefix(pathstring, "download") {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\"uploads.zip\"")
		zw := zip.NewWriter(w)
		var up Upload

		results := q.Run(c)
		for key, err := results.Next(&up); err != datastore.Done; key, err = results.Next(&up){
			name := strings.Replace(up.Name," ","_", -1)
			name = strings.Replace(name,"/","+", -1)
			name = name + "_" + key.StringID()+"/"

			fw, ferr := zw.Create(name+"comments.txt")
			check(ferr)
			io.WriteString(fw, up.Comments)

			fw, ferr = zw.Create(name+"report.pdf")
			check(ferr)
			fw.Write(up.PdfFile)

			fw, ferr = zw.Create(name+"src.zip")
			check(ferr)
			fw.Write(up.SrcZip)
			//dw.Close()
		}
		zw.Close()
		return
	}

	// var ups []*Upload
	// _, err := q.GetAll(c, &ups)
	// check(err)
	
	var uploads [] map[string]interface{}
	var up Upload

	results := q.Run(c)
	for key, err := results.Next(&up); err != datastore.Done; key, err = results.Next(&up){
		m := map[string]interface{}{
			"Name": up.Name, 
			"Time": up.Timestamp.Time().Format(time.RFC822), 
			"Email": up.KUemail,
			"Key": key.StringID(),
		}
		uploads = append(uploads, m)
	}

        err := adminTemplate.Execute(w, uploads);
        check(err)
}


// errorHandler wraps the argument handler with an error-catcher that
// returns a 500 HTTP error if the request fails (calls check with err non-nil).
func errorHandler(fn http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                defer func() {
                        if err, ok := recover().(os.Error); ok {
                                w.WriteHeader(http.StatusInternalServerError)
                                errorTemplate.Execute(w, err)
                        }
                }()
                fn(w, r)
        }
}

// check aborts the current execution if err is non-nil.
func check(err os.Error) {
        if err != nil {
                panic(err)
        }
}
