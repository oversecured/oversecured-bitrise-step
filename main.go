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
var BranchName = os.Getenv("branch_name")

const BaseUrl = "https://api.oversecured.com/v1"
var GetSignedLinkUrl = fmt.Sprintf("%s/upload/app", BaseUrl)
var AddVersionUrl = fmt.Sprintf("%s/integrations/%s/branches/%s/versions/add", BaseUrl, IntegrationID, BranchName)

const PlatformAndroid = "android"
const PlatformIOS = "ios"

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

func RequestErr(step string, resp *http.Response) (error) {
	var serverError ServerError
	ToJson(resp, &serverError)
	err := fmt.Errorf("oversecured: Step '%s' failed with code %d, server message: %s", step, resp.StatusCode, serverError.Message)
	return err
}

func ToJson(resp *http.Response, target interface{}) {
	json.NewDecoder(resp.Body).Decode(target)
}

func UploadFile(url string, path string) (error) {
    data, err := os.Open(path)
    if err != nil {
        log.Fatal(err)
    }
	req, err := http.NewRequest(http.MethodPut, url, data)
	if err != nil {
		return err
	}
	fi, err := data.Stat()
	if err != nil {
		return err
	}
	req.ContentLength = fi.Size()
	req.Header.Add("Content-Type", S3ContentType)
	s3Client := &http.Client{}
	resp, err := s3Client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return RequestErr("S3 Upload", resp)
	}
	return nil
}

func GetUploadInfo(platform string, path string) (SignResp, error) {
	signReq := SignReq {
		Platform: platform,
		FileName: path,
	}
	var signResp SignResp
	jsonValue, _ := json.Marshal(signReq)
	resp, err := client.Post(GetSignedLinkUrl, APIContentType, bytes.NewBuffer(jsonValue))
	if err != nil {
		return signResp, err
	}
	if resp.StatusCode != 200 {
		return signResp, RequestErr("Signed URL", resp)
	}
	ToJson(resp, &signResp)
	return signResp, nil
}

func AddVersion(bucketKey string, name string) (error) {
	uploadReq := VersionUpload {
		FileName: name,
		BucketKey: bucketKey,
	}
	jsonValue, _ := json.Marshal(uploadReq)
	resp, err := client.Post(AddVersionUrl, APIContentType, bytes.NewBuffer(jsonValue))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return RequestErr("Scan Version", resp)
	}
	return nil
}

func GetPlatform(path string) (string, error) {
	if strings.HasSuffix(path, ".apk") || strings.HasSuffix(path, ".aab") {
		return PlatformAndroid, nil
	}
	if strings.HasSuffix(path, ".zip") {
		return PlatformIOS, nil
	}
	err := fmt.Errorf("App file '%s' has invalid extension. Only '.apk', '.aab' and `.zip` are allowed.", path)
	return "", err
}

func ValidateAppPath(path string) (error) {
	if len(path) == 0 || !FileExists(path) {
		err := fmt.Errorf("App file '%s' doesn't exist. Make sure you've added the 'Android Build' step to the Workflow if you're scanning an Android app, and correctly zipped app sources if iOS.", path)
		return err
	}
	return nil
}

func FileExists(name string) bool {
    if _, err := os.Stat(name); err != nil {
        if os.IsNotExist(err) {
            return false
        }
    }
    return true
}

func run() (error) {
	fmt.Println("oversecured: file upload")

	var path = strings.TrimSpace(os.Getenv("app_path"))

	err := ValidateAppPath(path)
	if err != nil {
		return err
	}

	platform, err := GetPlatform(path)
	if err != nil {
		return err
	}

	var name = filepath.Base(path)
	
	signResp, err := GetUploadInfo(platform, name)
	if err != nil {
		return err
	}

	err = UploadFile(signResp.Url, path)
	if err != nil {
		return err
	}

	err = AddVersion(signResp.BucketKey, name)
	if err != nil {
		return err
	}

	fmt.Println("oversecured: success")
	return nil
}

func main() {
    if err := run(); err != nil {
	    log.Fatal(err)
        os.Exit(1)
    }
	os.Exit(0)
}