package main

import (
	"github.com/original-david-knight/go_wild/agent_data"
)

func handleImageCommand(cm data.CommandMessage, ctx commandContext) commandResult {
	imgPath := cmdArg(cm, "path")
	if imgPath == "" {
		output.Error("Usage: /image <path>")
		return cmdContinue
	}
	img, err := loadImageFromFile(imgPath)
	if err != nil {
		output.Error("Error loading image: %v", err)
		return cmdContinue
	}
	*ctx.pendingImage = img
	output.SystemSuccess("Image attached: %s (%d bytes, %s)", img.name, len(img.data), img.mimeType)
	output.System("Enter your prompt to send with the image:")
	return cmdContinue
}

func handlePasteCommand(_ data.CommandMessage, ctx commandContext) commandResult {
	img, err := loadImageFromClipboard()
	if err != nil {
		output.Error("Error pasting image: %v", err)
		return cmdContinue
	}
	*ctx.pendingImage = img
	output.SystemSuccess("Image pasted from clipboard (%d bytes, %s)", len(img.data), img.mimeType)
	output.System("Enter your prompt to send with the image:")
	return cmdContinue
}

func handleFileCommand(cm data.CommandMessage, ctx commandContext) commandResult {
	filePath := cmdArg(cm, "path")
	if filePath == "" {
		output.Error("Usage: /file <path>")
		output.System("Tip: Use Tab for path completion")
		return cmdContinue
	}
	file, err := loadFile(filePath)
	if err != nil {
		output.Error("Error loading file: %v", err)
		return cmdContinue
	}
	*ctx.pendingImage = file
	output.SystemSuccess("File attached: %s (%d bytes, %s)", file.name, len(file.data), file.mimeType)
	output.System("Enter your prompt to send with the file:")
	return cmdContinue
}
