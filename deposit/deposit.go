// Copyright 2011 Ken Friis Larsen. All rights reserved.
package deposit

import (
	"archive/zip"
//	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net/http"
	"text/template"
	"time"
	//	"strconv"
	//	"strings"
	"regexp"
)

// These imports were added for deployment on App Engine.
import (
	"appengine"
	"appengine/blobstore"
	"appengine/datastore"
	"appengine/mail"
	"appengine/user"
)

var (
	uploadTemplate = template.Must(template.ParseFiles("apupload.html"))
	viewTemplate   = template.Must(template.ParseFiles("singleupload.html"))
	adminTemplate  = template.Must(template.ParseFiles("admin.html"))
	errorTemplate  = template.Must(template.ParseFiles("error.html"))
)

const viewPath = "/upload/"

func init() {
	http.HandleFunc("/", errorHandler(upload))
	http.HandleFunc("/addupload", errorHandler(addupload))
	http.HandleFunc(viewPath, errorHandler(showupload))
	http.HandleFunc("/admin/", errorHandler(admin))
	http.HandleFunc("/admin/download", errorHandler(download))
}

// Upload is the type used to hold the uploaded data in the datastore.
type Upload struct {
	Name      string
	KUemail   string
	Comments  string
	Timestamp time.Time
	PdfFile   appengine.BlobKey
	SrcZip    appengine.BlobKey
}

var nameValidator = regexp.MustCompile("[^a-zA-Z0-9._:@]")

func safeName(name string) string {
	return nameValidator.ReplaceAllString(name, "_")
}

var emailValidator = regexp.MustCompile("^[a-zA-Z0-9]+@[a-z.A-Z0-9]*ku.dk$")

func validate(r *http.Request) (name string, kuemail string) {
	_, _, err := blobstore.ParseUpload(r)
	check(err)

	if name := r.FormValue("name"); name == "" {
		panic(errors.New("Please provide a name"))
	}
	if kuemail := r.FormValue("kuemail"); !emailValidator.MatchString(kuemail) {
		panic(errors.New("Please provide a proper KU email"))
	}
	if _, _, err := r.FormFile("pdffile"); err != nil {
		panic(errors.New("Please provide a PDF file"))
	}
	if _, _, err := r.FormFile("zipfile"); err != nil {
		panic(errors.New("Please provide a zip file"))
	}
	return
}

// upload is the HTTP handler for "/".
func upload(w http.ResponseWriter, r *http.Request) {
	// No upload; show the upload form.
	c := appengine.NewContext(r)
	uploadURL, err := blobstore.UploadURL(c, "/addupload", nil)
	check(err)

	err = uploadTemplate.Execute(w, uploadURL)
	check(err)
}


// add
func addupload(w http.ResponseWriter, r *http.Request) {
//	name, kuemail := validate(r)

	blobs, others, err := blobstore.ParseUpload(r)
	check(err)

	var name string
	var kuemail string
	if name = others.Get("name"); name == "" {
		panic(errors.New("Please provide a name"))
	}
	if kuemail = others.Get("kuemail"); !emailValidator.MatchString(kuemail) {
		panic(errors.New("Please provide a prober KU email"))
	}
	if len(blobs["pdffile"]) == 0 {
		panic(errors.New("Please provide a PDF file"))
	}
	if len(blobs["zipfile"]) == 0 {
		panic(errors.New("Please provide a zip file"))
	}

	pdffile := blobs["pdffile"]
	zipfile := blobs["zipfile"]

	up := Upload{
		Name:      name,
		KUemail:   kuemail,
		Comments:  others.Get("comments"),
		Timestamp: time.Now(),
		PdfFile:   pdffile[0].BlobKey, //pdfbuf.Bytes(),
		SrcZip:    zipfile[0].BlobKey, //zipbuf.Bytes(),
	}

	// Create an App Engine context for the client's request.
	c := appengine.NewContext(r)

	// Save the upload under a (hopefully) unique key, a hash of
	// the data
	key := datastore.NewKey(c, "Upload", keyOf(c, &up), 0, nil)
	_, err = datastore.Put(c, key, &up)
	check(err)

	url := "http://filenotary.appspot.com" + viewPath + key.StringID()
	addr := kuemail
	msg := &mail.Message{
		Sender:  "Ken Friis Larsen <kflarsen@diku.edu>",
		To:      []string{addr},
		Subject: "You upload is registered with the File Notary",
		Body:    fmt.Sprintf(uploadMessage, up.Name, url),
	}
	mail.Send(c, msg)
	//Ignore if the sending of mail goes wrong
	//check(err)

	// Redirect to /upload/ using the key.
	http.Redirect(w, r, viewPath+key.StringID(), http.StatusFound)
}

// keyOf returns (part of) the SHA-1 hash of the data, as a hex string.
func keyOf(c appengine.Context, up *Upload) string {
	sha := sha1.New()
	io.WriteString(sha, up.Name)
	io.WriteString(sha, up.KUemail)
	io.WriteString(sha, up.Comments)
	io.WriteString(sha, up.Timestamp.Format(time.RFC822))
	io.Copy(sha, blobstore.NewReader(c,up.PdfFile))
	io.Copy(sha, blobstore.NewReader(c,up.SrcZip))
	return fmt.Sprintf("%x", string(sha.Sum(nil))[0:10])
}

const uploadMessage = `
Thank you %s

Your upload is now registered, you can see what is registred at:
    %s

Cheers,

--Ken
`

// showupload is the HTTP handler for a single upload; it handles "/upload/".
func showupload(w http.ResponseWriter, r *http.Request) {
	keystring := r.URL.Path[len(viewPath):]
	c := appengine.NewContext(r)
	key := datastore.NewKey(c, "Upload", keystring, 0, nil)
	up := new(Upload)
	err := datastore.Get(c, key, up)
	check(err)

	pdfSha, zipSha := shaOf(c, up.PdfFile, up.SrcZip)

	m := map[string]interface{}{
		"Name":   up.Name,
		"Time":   up.Timestamp.Format(time.RFC850),
		"Comments": up.Comments,
		"Key":    keystring,
		"PdfSha": pdfSha,
		"ZipSha": zipSha,
	}

	err = viewTemplate.Execute(w, m)
	check(err)
}

func shaOf(c appengine.Context, pdfFile appengine.BlobKey, srcZip appengine.BlobKey) (string, string) {
	sha := sha1.New()
	io.Copy(sha, blobstore.NewReader(c, pdfFile))
	pdfSha := fmt.Sprintf("%x", sha.Sum(nil))
	sha.Reset()
	io.Copy(sha, blobstore.NewReader(c, srcZip))
	zipSha := fmt.Sprintf("%x", sha.Sum(nil))
	return pdfSha, zipSha
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
	q := datastore.NewQuery("Upload").Order("Timestamp").Order("KUemail")

	// var ups []*Upload
	// _, err := q.GetAll(c, &ups)
	// check(err)

	var uploads []map[string]interface{}
	var up Upload

	results := q.Run(c)
	for key, err := results.Next(&up); err != datastore.Done; key, err = results.Next(&up) {
		m := map[string]interface{}{
			"Name":  up.Name,
			"Time":  up.Timestamp.Format(time.RFC822),
			"Email": up.KUemail,
			"Key":   key.StringID(),
		}
		uploads = append(uploads, m)
	}

	err := adminTemplate.Execute(w, uploads)
	check(err)
}

func download(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	u := user.Current(c)
	if u == nil {
		url, err := user.LoginURL(c, r.URL.String())
		check(err)
		w.Header().Set("Location", url)
		w.WriteHeader(http.StatusFound)
		return
	}
	q := datastore.NewQuery("Upload").Order("Timestamp").Order("KUemail")

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"uploads.zip\"")
	//		w.WriteHeader(http.StatusFound)
	zw := zip.NewWriter(w)
	var up Upload

	results := q.Run(c)
	for key, err := results.Next(&up); err != datastore.Done; key, err = results.Next(&up) {
		stamp := up.Timestamp.Format(time.RFC3339)
		//			part := strings.SplitN(up.KUemail,"@",2)[0]
		name := safeName(up.Name + "_" + up.KUemail)

		name = name + "/" + stamp + "_" + key.StringID() + "/"

		fw, ferr := zw.Create(name + "comments.txt")
		check(ferr)
		io.WriteString(fw, up.Comments)
		io.WriteString(fw, "\n\n"+up.Name+" ("+up.KUemail+")\n")

		addBlobToZip(c, zw, up.PdfFile, name, "report.pdf")
		addBlobToZip(c, zw, up.SrcZip, name, "src.zip")
	}
	zw.Close()
}

func addBlobToZip(c appengine.Context, zw *zip.Writer, key appengine.BlobKey, path string, defName string) {
	blobinfo, err := blobstore.Stat(c, key)
	check(err)
	fname := safeName(blobinfo.Filename)
	if len(fname) == 0 {
		fname = defName
	}

	fw, ferr := zw.Create(path + fname)
	check(ferr)
	io.Copy(fw, blobstore.NewReader(c, key))
}

// errorHandler wraps the argument handler with an error-catcher that
// returns a 500 HTTP error if the request fails (calls check with err non-nil).
func errorHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err, ok := recover().(error); ok {
				w.WriteHeader(http.StatusInternalServerError)
				errorTemplate.Execute(w, err)
			}
		}()
		fn(w, r)
	}
}

// check aborts the current execution if err is non-nil.
func check(err error) {
	if err != nil {
		panic(err)
	}
}
