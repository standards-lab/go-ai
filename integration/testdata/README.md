# integration testdata

The media the integration tier sends to the vision and audio endpoints. Both files were made in
spike-harness-driver (https://github.com/JaimeStill/spike-harness-driver), where they are
`clutch/examples/media/shapes.png` and `clutch/examples/media/phrase.wav`, and are copied here
unchanged from its commit `79af481c3b3c886727a907b9380685f2fada6f4c`. That directory's README
says how each was made:

- `shapes.png`: a red circle, a blue square, and a green triangle on white, 480 pixels square,
  drawn with the Go standard library by `clutch/examples/media/shapes.go`, which regenerates it
  exactly. `TestImageDescription` asks the vision model to describe it and wants each shape named.
- `phrase.wav`: one speaker saying "The access code is seven four two nine.", 16 kHz mono 16-bit
  PCM, 6.05 seconds, kept short because Gemma 4 E4B loops on clips longer than about 30 seconds.
  The README gives two ways to make it, espeak-ng resampled by ffmpeg, or a recording of the
  phrase:

  ```sh
  ffmpeg -y -f pulse -i default -t 6 -ar 16000 -ac 1 phrase.wav
  ```

  The commit that added it says it was made by hand per that README, six seconds of 16 kHz mono,
  which is the recording's form. `TestTranscribeChat` and `TestTranscribeRoute` want its words
  back, and `TestAudioInResponsesRefused` sends it where it is refused.

With spike-harness-driver checked out beside this repository, this prints nothing when the copies
match their source:

```sh
for f in shapes.png phrase.wav; do
  git -C ../spike-harness-driver show 79af481c3b3c886727a907b9380685f2fada6f4c:clutch/examples/media/$f |
    cmp - integration/testdata/$f
done
```
