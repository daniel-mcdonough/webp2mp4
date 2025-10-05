# webp2mp4

Converts animated webps to mp4
ffmpeg has issues converting animated webps and some steps are required to get it to play nice. This program does that for you.

This is literally just a script in Golang form to make it more portable. It uses ffmpeg and Imagemagick for processing and will dump frames in a /tmp/webp2mp4 directory to then reassemble them(if the first method fails). If the webp has any odd dimensions, it will make it even so it works with h.264.

## Install

Needs ffmpeg installed. Imagemagick isn't a hard requirement but you'll need it most likely.

Download the file from the releases page and put it in a directory in your PATH or execute it directly.

You can build it yourself like so:

```
go build -o webp2mp4 main.go
```

## Usage

```
./webp2mp4 -i animated.webp
```


### Options

- `-o output.mp4` - specify output name
- `-fps 30` - framerate (default 30)
- `-b 2M` - bitrate
- `-v` - verbose
- `-method extract` - This will extract frames into a temp folder and then assemble the video with it

## Notes

Handles odd dimensions automatically since h264 needs even numbers

If it fails, try `-method extract` which uses imagemagick as backup.

Tested on Arch