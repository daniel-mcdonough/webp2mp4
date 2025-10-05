package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

func main() {
	var (
		input   string
		output  string
		fps     int
		bitrate string
		verbose bool
		method  string
	)

	flag.StringVar(&input, "i", "", "Input animated WebP file (required)")
	flag.StringVar(&output, "o", "", "Output MP4 file (optional, defaults to input name with .mp4)")
	flag.IntVar(&fps, "fps", 30, "Frame rate for output video")
	flag.StringVar(&bitrate, "b", "2M", "Video bitrate (e.g., 2M, 5M)")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.StringVar(&method, "method", "auto", "Conversion method: 'auto', 'extract', or 'direct'")
	flag.Parse()

	if input == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -i input.webp [-o output.mp4] [-fps 30] [-b 2M] [-v]\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	if output == "" {
		ext := filepath.Ext(input)
		output = strings.TrimSuffix(input, ext) + ".mp4"
	}

	if err := convertWebPToMP4(input, output, fps, bitrate, verbose, method); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Successfully converted %s to %s\n", input, output)
}

func convertWebPToMP4(input, output string, fps int, bitrate string, verbose bool, method string) error {
	// Check if input file exists
	if _, err := os.Stat(input); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", input)
	}

	// Determine conversion method
	if method == "auto" {
		// Try direct conversion first, fall back to extraction if it fails
		if err := convertDirectly(input, output, fps, bitrate, verbose); err != nil {
			if verbose {
				fmt.Printf("Direct conversion failed, trying frame extraction method: %v\n", err)
			}
			return convertViaExtraction(input, output, fps, bitrate, verbose)
		}
		return nil
	} else if method == "extract" {
		return convertViaExtraction(input, output, fps, bitrate, verbose)
	} else {
		return convertDirectly(input, output, fps, bitrate, verbose)
	}
}

func convertViaExtraction(input, output string, fps int, bitrate string, verbose bool) error {
	// Create temporary directory for frames
	tempDir, err := ioutil.TempDir("", "webp2mp4_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if verbose {
		fmt.Printf("Extracting frames to: %s\n", tempDir)
	}

	// Extract frames using webpmux
	framePattern := filepath.Join(tempDir, "frame_%03d.png")
	extractCmd := exec.Command("webpmux", "-get", "frame", "0", input, "-o", "-")

	// Try alternative extraction method using ffmpeg to extract frames
	extractArgs := []string{
		"-i", input,
		"-vsync", "0",
		framePattern,
	}

	extractCmd = exec.Command("ffmpeg", extractArgs...)
	if verbose {
		extractCmd.Stdout = os.Stdout
		extractCmd.Stderr = os.Stderr
		fmt.Printf("Extracting frames: ffmpeg %s\n", strings.Join(extractArgs, " "))
	}

	if err := extractCmd.Run(); err != nil {
		// If frame extraction fails, try using imagemagick as fallback
		if verbose {
			fmt.Println("FFmpeg extraction failed, trying ImageMagick...")
		}
		convertCmd := exec.Command("convert", input, "-coalesce", framePattern)
		if err := convertCmd.Run(); err != nil {
			return fmt.Errorf("failed to extract frames: %w", err)
		}
	}

	// Check if we got any frames
	frames, err := filepath.Glob(filepath.Join(tempDir, "frame_*.png"))
	if err != nil || len(frames) == 0 {
		return fmt.Errorf("no frames extracted from WebP")
	}

	if verbose {
		fmt.Printf("Extracted %d frames\n", len(frames))
	}

	// Get dimensions from first frame
	firstFrame := frames[0]
	width, height, err := getPNGDimensions(firstFrame)
	if err != nil {
		return fmt.Errorf("failed to get frame dimensions: %w", err)
	}

	// Adjust dimensions to be even (required for h264)
	adjustedWidth := makeEven(width)
	adjustedHeight := makeEven(height)

	if verbose {
		fmt.Printf("Frame dimensions: %dx%d\n", width, height)
		if adjustedWidth != width || adjustedHeight != height {
			fmt.Printf("Adjusted dimensions: %dx%d (made even for h264 compatibility)\n", adjustedWidth, adjustedHeight)
		}
	}

	// Build ffmpeg command to create video from frames
	args := []string{
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", filepath.Join(tempDir, "frame_%03d.png"),
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-b:v", bitrate,
	}

	// Add scaling filter if dimensions need adjustment
	if adjustedWidth != width || adjustedHeight != height {
		scaleFilter := fmt.Sprintf("scale=%d:%d:flags=lanczos", adjustedWidth, adjustedHeight)
		args = append(args, "-vf", scaleFilter)
	}

	// Add output options
	args = append(args,
		"-preset", "medium",
		"-movflags", "+faststart",
		"-y", // Overwrite output file
		output,
	)

	// Execute ffmpeg
	cmd := exec.Command("ffmpeg", args...)

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("Creating video: ffmpeg %s\n", strings.Join(args, " "))
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed to create video: %w", err)
	}

	return nil
}

func convertDirectly(input, output string, fps int, bitrate string, verbose bool) error {
	// Get dimensions and adjust if needed
	width, height, err := getWebPDimensions(input)
	if err != nil {
		// If we can't get dimensions, try without pre-checking
		width, height = 0, 0
	}

	// Build ffmpeg command with special flags for animated WebP
	args := []string{
		"-f", "webp_pipe",
		"-i", input,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-r", fmt.Sprintf("%d", fps),
		"-b:v", bitrate,
	}

	// Add scaling filter if we know dimensions need adjustment
	if width > 0 && height > 0 {
		adjustedWidth := makeEven(width)
		adjustedHeight := makeEven(height)

		if verbose {
			fmt.Printf("Original dimensions: %dx%d\n", width, height)
			if adjustedWidth != width || adjustedHeight != height {
				fmt.Printf("Adjusted dimensions: %dx%d (made even for h264 compatibility)\n", adjustedWidth, adjustedHeight)
			}
		}

		if adjustedWidth != width || adjustedHeight != height {
			scaleFilter := fmt.Sprintf("scale=%d:%d:flags=lanczos", adjustedWidth, adjustedHeight)
			args = append(args, "-vf", scaleFilter)
		}
	} else {
		// If we don't know dimensions, use a filter to ensure even dimensions
		args = append(args, "-vf", "scale='trunc(iw/2)*2:trunc(ih/2)*2'")
	}

	// Add output options
	args = append(args,
		"-preset", "medium",
		"-movflags", "+faststart",
		"-y", // Overwrite output file
		output,
	)

	// Execute ffmpeg
	cmd := exec.Command("ffmpeg", args...)

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("Running command: ffmpeg %s\n", strings.Join(args, " "))
	} else {
		// Capture output to check for errors
		cmdOutput, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(cmdOutput))
		}
	}

	if err := cmd.Run(); err != nil && !verbose {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	return nil
}

func getWebPDimensions(filename string) (int, int, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}

func getPNGDimensions(filename string) (int, int, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}

func makeEven(n int) int {
	if n%2 != 0 {
		return n + 1
	}
	return n
}

func checkDependencies() error {
	// Check if ffmpeg is installed
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is not installed or not in PATH")
	}
	// Optional: check for imagemagick (convert command) for fallback
	if _, err := exec.LookPath("convert"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: ImageMagick (convert) not found. Some animated WebP files might not convert properly.\n")
	}
	return nil
}

func init() {
	if err := checkDependencies(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please install ffmpeg first.\n")
		os.Exit(1)
	}
}