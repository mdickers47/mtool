#!/usr/bin/python
"""
A hack to convert a collection of FLAC files in the style of www.etree.org
into a single file with cuesheet and tags, in the style of flacenstein.

This is probably going to end up being a tour of pyflac: we need to find
the stream lengths from the metadata, decode the streams, re-encode them
in one long stream, and construct a cuesheet in the new file.

Copyright (c) 2005 Michael A. Dickerson.  Modification and redistribution
are permitted under the terms of the GNU General Public License, version 2.

2017-04-13 MAD: Well pyflac was abandoned 7 years ago, so let's rewrite
                for whatever CADT library will exist for the next five
                minutes.
                (2 hours later) forget it.  There are unsurprisingly
                a handful of them, all half implemented and all with
                random stale versions packaged into ubuntu or pip.
                As usual, the mistake was using any software interface
                whatsoever, and as usual the solution is os.system().
"""

import os
import sys
import wave

BIN_FLAC = 'flac'
BIN_METAFLAC = 'metaflac'
CD_SAMPLES_PER_FRAME = 588 # 44100 samples/sec / 75 frames/sec
SECONDS_PER_SEEKPOINT = 10.031 # magic number? observed to be what flac does

class FlacCombineError(Exception): pass

class outstream:

    def __init__(self):
        self.sample_rate = None
        self.channels = None
        self.bytes_per_sample = None
        self.total_samples = 0 # accumulated stream length, in samples
        self.filename = 'combined.wav' # TO DO: be less lame
        self.encoder = None
        self.track_offsets = []
        self.track_count = 0
        self.last_progress = None
        self.bufsize = 2048
        self.outpipe = None

    def pad_out_cd_frame(self):
        """
        We need to make track boundaries and the end of the wav stream
        align exactly with a CD frame boundary, or we find bugs and
        random behavior in many decoders, CD burners, etc.
        """
        pos = self.total_samples
        if pos % CD_SAMPLES_PER_FRAME:
            padding = CD_SAMPLES_PER_FRAME - (pos % CD_SAMPLES_PER_FRAME)
            print('WARNING: Adding %d bytes padding ' % padding
                  + 'to align to CD frame boundary.')
            nbytes = padding * self.bytes_per_sample * self.channels
            self.process_samples(b'\0' * nbytes)        
        assert self.total_samples % CD_SAMPLES_PER_FRAME == 0
        
    def start_track(self):
        self.pad_out_cd_frame()
        pos = self.total_samples
        print('Track starts at %d samples (%d sec)' \
              % (pos, pos / self.sample_rate))
        self.track_offsets.append(pos)

    def init(self):
        if self.outpipe == None:
            self.outpipe = wave.open(self.filename, 'w')
            self.outpipe.setnchannels(self.channels)
            self.outpipe.setsampwidth(self.bytes_per_sample)
            self.outpipe.setframerate(self.sample_rate)
            print('output pipe to %s is ready' % self.filename)
                
    def set_channels(self, n):
        assert type(n) == int
        if self.channels == None or self.channels == n:
            self.channels = n
        else:
            raise FlacCombineError('Number of channels changed from %d to %d' %
                                  (self.channels, n))

    def set_sample_rate(self, n):
        assert type(n) == int
        if self.sample_rate == None or self.sample_rate == n:
            self.sample_rate = n
        else:
            raise FlacCombineError('Sample rate changed from %d to %d' %
                                   (self.sample_rate, n))

    def set_bytes_per_sample(self, n):
        assert type(n) == int
        if self.bytes_per_sample == None or self.bytes_per_sample == n:
            self.bytes_per_sample = n
        else:
            raise FlacCombineError('Bit depth changed from %d to %d' %
                                   (self.bytes_per_sample*8, n))

    def process_samples(self, data):
        self.outpipe.writeframes(data)
        bytes_per_wav_frame = self.bytes_per_sample * self.channels
        if (len(data) % bytes_per_wav_frame):
            raise FlacCombineError('Size of data written not '
                                   'a multiple of wave frame size.')
        self.total_samples += len(data) // bytes_per_wav_frame

    def finish(self):
        self.pad_out_cd_frame()
        self.outpipe.close()
        self.outpipe = None

        # NB that this padding is for metaflac to stuff data into; it
        # has nothing to do with the wav stream padding we have been
        # creating to force CD frame alignment.
        print('Compressing...')
        os.system('%s --padding=%d --delete-input-file %s' % \
                  (BIN_FLAC, 128*1024, self.filename))
        print('done.')
        # I know this sucks.
        self.filename = self.filename.replace('.wav', '.flac')
        
        print('Creating seek table...', end='')
        os.system('%s --add-seekpoint=%fs %s' %
                  (BIN_METAFLAC, SECONDS_PER_SEEKPOINT, self.filename))
        print('done.')

        print('Writing cuesheet...', end='')
        cue = os.popen('%s --import-cuesheet-from=- %s' %
                       (BIN_METAFLAC, self.filename), 'w')
        cue.write('FILE "dummy.wav" WAVE\n')
        for i, off in enumerate(self.track_offsets):
            cue.write('  TRACK %02d AUDIO\n' % (i + 1))
            assert off % CD_SAMPLES_PER_FRAME == 0
            frames = off / CD_SAMPLES_PER_FRAME
            (secs, frames) = divmod(frames, 75)
            (mins, secs) = divmod(secs, 60)
            cue.write('    INDEX 01 %02d:%02d:%02d\n' % (mins, secs, frames))
        cue.close()
        print('done.')

        print('Writing VORBIS tags...', end='')
        tags = os.popen('%s --import-tags-from=- %s' %
                        (BIN_METAFLAC, self.filename), 'w')
        tags.write('ARTIST=\n'
                   'ALBUM=\n'
                   'DATE=\n')
        for i, _ in enumerate(self.track_offsets):
            tags.write('TITLE[%d]=\n' % (i + 1))
        tags.close()
        print('done.')
        
# ------- work starts here -------

if __name__ == '__main__':
    if len(sys.argv) == 2:
        # Probably doesn't work
        tracklist = open(sys.argv[1], 'r').readlines()
    else:
        tracklist = [ x.strip() for x in sys.argv[1:] ]
    out = outstream()

    for infile in tracklist:
        print('Reading file: %s' % infile)

        if infile.endswith('wav'):

            bufsize = 1024
            wav = wave.open(infile, 'r')
            out.set_channels(wav.getnchannels())
            out.set_bytes_per_sample(wav.getsampwidth())
            out.set_sample_rate(wav.getframerate())
            out.start_track()
            out.init()

            data = wav.readframes(bufsize)
            while data:
                out.process_samples(data)
                data = wav.readframes(bufsize)
            wav.close()

        else:
            print('skipping: name ends with not wav')

    out.finish()
