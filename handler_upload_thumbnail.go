package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	// Make sure the given videoID is a correct one
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

	// Get the file metadata and confirm the user owns the video
	fileMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse file metadata", err)
		return
	}
	if fileMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User does not own this video", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// Open the file and set the mediaType
	const maxMemory = 10 << 20 // Same as 10 * 1024 * 1024, or 10 MB
	r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	mediaType := header.Header.Get("Content-Type")
	defer file.Close()

	// Load the image data into a Base 64 string, then a data URL
	thumbnailData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse file data", err)
		return
	}

	thumbnailDataB64 := base64.StdEncoding.EncodeToString(thumbnailData)

	thumbnailDataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, thumbnailDataB64)

	// Update the thumbnail URL

	fileMetadata.ThumbnailURL = &thumbnailDataURL

	err = cfg.db.UpdateVideo(fileMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video metadata", err)
		return
	}

	// Send... something on.

	respondWithJSON(w, http.StatusOK, fileMetadata)
}
