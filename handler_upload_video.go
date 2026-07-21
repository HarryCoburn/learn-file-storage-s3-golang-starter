package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// Set maximum video size
	const maxMemory = 10 << 30 // 10 GB

	// Get the video ID string
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	// Authenticate the user.
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// Get the video metadata and confirm the user owns the video
	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse file metadata", err)
		return
	}
	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User does not own this video", err)
		return
	}

	// Authentication and authorization finished. Proceed.
	fmt.Println("uploading video", videoID, "by user", userID)

	// Open video file
	r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	mediaType := header.Header.Get("Content-Type")
	defer file.Close()

	// Confirm it is mp4
	typeCheck, _, err := mime.ParseMediaType(mediaType)
	if typeCheck != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "video is not in mp4", err)
		return
	}

	// Create a temporary file and save the uploaded file to it.
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	io.Copy(tempFile, file)
	tempFile.Seek(0, io.SeekStart) // Move file pointer to the start for re-reading

	// Move the temp file to S3

	// Make the filename
	fileNameSlice := make([]byte, 32)
	rand.Read(fileNameSlice)
	fileNameBase := base64.RawURLEncoding.EncodeToString(fileNameSlice)
	videoDim, err := getVideoAspectRatio(tempFile.Name())
	var aspect string
	switch videoDim {
	case "16:9":
		aspect = "landscape"
	case "9:16":
		aspect = "portrait"
	default:
		aspect = "other"
	}

	fileName := fmt.Sprint(aspect + "/" + fileNameBase + ".mp4")

	params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        tempFile,
		ContentType: &typeCheck,
	}

	cfg.s3Client.PutObject(r.Context(), &params)

	// Make the URL for the database

	videoDataURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileName)
	videoMetadata.VideoURL = &videoDataURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video metadata", err)
		return
	}

	// Send metadata on

	respondWithJSON(w, http.StatusOK, videoMetadata)
}

func getVideoAspectRatio(filePath string) (string, error) {
	ffmpegCommand := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	b := bytes.Buffer{}
	ffmpegCommand.Stdout = &b
	if err := ffmpegCommand.Run(); err != nil {
		return "", nil
	}

	type VideoDimensions struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	}
	type probeOutput struct {
		Streams []VideoDimensions `json:"streams"`
	}

	var out probeOutput
	if err := json.Unmarshal(b.Bytes(), &out); err != nil {
		return "", err
	}

	for _, s := range out.Streams {
		if s.CodecType == "video" && s.Height != 0 {
			ratio := float64(s.Width) / float64(s.Height)
			switch {
			case math.Abs(ratio-16.0/9.0) < 0.01:
				return "16:9", nil
			case math.Abs(ratio-9.0/16.0) < 0.01:
				return "9:16", nil
			default:
				return "other", nil
			}
		}
	}

	return "", fmt.Errorf("no video stream found in %s", filePath)

}
