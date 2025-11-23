package main

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

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

	// Limit the upload file size using a MaxBytesReader
	const uploadLimit = 1 << 30 // 1Gb
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	// Get the video from the DB
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to find video", err)
		return
	}

	// Make sure authenticated users is the owner
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized for this video", err)
		return
	}

	// read from "video" HTML form input name
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	// Get content type
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse media type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Unsupported media type, only supports mp4", err)
		return
	}

	// create temp file on disk
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Write the temp file
	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error saving temp file", err)
		return
	}

	// get aspect ratio and folder name
	folder := "other"
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get aspect ratio", err)
		return
	}
	if aspectRatio == "16:9" {
		folder = "landscape"
	} else if aspectRatio == "9:16" {
		folder = "portrait"
	}

	// get the video filename
	s3Key := path.Join(folder, getAssetPath(mediaType))

	// create file in temp dir optimized for faster start
	processedTempFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error optimizing temp file", err)
		return
	}
	defer os.Remove(processedTempFilePath)

	// load the new temp file
	processedTempFile, err := os.Open(processedTempFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error loading processed temp file", err)
		return
	}
	defer processedTempFile.Close()

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(s3Key),
		Body:        processedTempFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload video to s3", err)
		return
	}

	// Update video url with the one from S3r
	s3VideoUrl := cfg.getObjectURL(s3Key)
	video.VideoURL = &s3VideoUrl

	// Update the video in the DB
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
