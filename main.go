package main

import (
	"fmt"
	"os"
	"net/http"
	"encoding/json"
	"bytes"
	"path/filepath"
	"log"
	"strings"
)

var AccessToken = os.Getenv("access_token")
var IntegrationID = os.Getenv("integration_id")

const BaseUrl = "https://api.oversecured.com/v1"
var GetSignedLinkUrl = fmt.Sprintf("%s/upload/app", BaseUrl)
var AddVersionUrl = fmt.Sprintf("%s/integrations/%s/versions/add", BaseUrl, IntegrationID)

const Platform = "android"
const APIContentType = "application/json"
const S3ContentType = "application/octet-stream"

type transport struct {
    headers map[string]string
    base    http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
    for k, v := range t.headers {
        req.Header.Add(k, v)
    }
    base := t.base
    if base == nil {
        base = http.DefaultTransport
    }
    return base.RoundTrip(req)
}

var client = &http.Client{
        Transport: &transport{
            headers: map[string]string{
                "Authorization": AccessToken,
            },
        },
    }

type SignReq struct {
	Platform 	string `json:"platform"`
	FileName 	string `json:"file_name"`
}

type SignResp struct {
	BucketKey 	string `json:"bucket_key"`
	Url 		string `json:"url"`
}

type VersionUpload struct {
	FileName 	string `json:"file_name"`
	BucketKey 	string `json:"bucket_key"`
}

type ServerError struct {
	Message string `json:"message"`
}

func RequestErr(step string, resp *http.Response) {
	var serverError ServerError
	ToJson(resp, &serverError)
	err := fmt.Errorf("oversecured: Step '%s' failed with code %d, server message: %s", step, resp.StatusCode, serverError.Message)
	log.Fatal(err)
}

func ToJson(resp *http.Response, target interface{}) {
	json.NewDecoder(resp.Body).Decode(target)
}

func UploadFile(url string, path string) {
    data, err := os.Open(path)
    if err != nil {
        log.Fatal(err)
    }
	req, err := http.NewRequest(http.MethodPut, url, data)
	if err != nil {
		log.Fatal(err)
	}
	fi, err := data.Stat()
	if err != nil {
	    log.Fatal(err)
	}
	req.ContentLength = fi.Size()
	req.Header.Add("Content-Type", S3ContentType)
	s3Client := &http.Client{}
	resp, err := s3Client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != 200 {
		RequestErr("S3 Upload", resp)
	}
}

func GetUploadInfo(name string) (SignResp) {
	signReq := SignReq {
		Platform: Platform,
		FileName: name,
	}
	jsonValue, _ := json.Marshal(signReq)
	resp, err := client.Post(GetSignedLinkUrl, APIContentType, bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != 200 {
		RequestErr("Signed URL", resp)
	}
	var signResp SignResp
	ToJson(resp, &signResp)
	return signResp
}

func AddVersion(bucketKey string, name string) {
	uploadReq := VersionUpload {
		FileName: name,
		BucketKey: bucketKey,
	}
	jsonValue, _ := json.Marshal(uploadReq)
	resp, err := client.Post(AddVersionUrl, APIContentType, bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != 200 {
		RequestErr("Scan Version", resp)
	}
}

func ValidateAppPath(path string) {
	if !strings.HasSuffix(path, ".apk") && !strings.HasSuffix(path, ".aab") {
		err := fmt.Errorf("App file '%s' has invalid extension. Only '.apk' and '.aab' are allowed.", path)
		log.Fatal(err)
	}
	if len(path) == 0 || !FileExists(path) {
		err := fmt.Errorf("App file '%s' doesn't exist. Make sure you've added the 'Android Build' step to the Workflow.", path)
		log.Fatal(err)
	}
}

func FileExists(name string) bool {
    if _, err := os.Stat(name); err != nil {
        if os.IsNotExist(err) {
            return false
        }
    }
    return true
}

func main() {
	fmt.Println("oversecured: starting version upload")

	var path = strings.TrimSpace(os.Getenv("app_path"))
	ValidateAppPath(path)

	var name = filepath.Base(path)
	
	signResp := GetUploadInfo(name)
	UploadFile(signResp.Url, path)
	AddVersion(signResp.BucketKey, name)

	fmt.Println("oversecured: success")
	os.Exit(0)
}