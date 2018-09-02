#!/usr/bin/env python
"""dvdrip.py - Dump video titles from DVD to multiplexed MP4 files.

No conversion or re-coding is done, yet this still gets very
complicated, because DVDs are hyper-optimized to be played by machines
of very little brain.  The MPEG-4 video codec is inefficient compared
to 2018 options, and the bitrates and variations discovered on
commercial DVDs vary wildly.  The resulting dump file will be much
bigger and worse-performing than can be achieved by recoding
afterwards.

Subtitle and audio streams are interleaved so that a player can avoid
seeking or buffering.  The MP4 container file can also multiplex them,
but the result will not be useful unless the streams have all been
labeled with metadata (so that you can find the English audio again,
for example).  DVDs are sloppy about labeling the streams, so many
hacks have to be applied.  These hacks assume one of two basic
schemes: it's a movie (where we want to record year and title), or
it's a TV show (where we want year, show name, and multiple episode
names).

DVD menus are basically bitmaps overlaid with a spaghetti mess of
GOTOs, so we can't interpret them.  This means you just have to guess
from the TOC of video titles, which one is the video you want.  TV
DVDs with many same-length titles on one disc will end up out of order
sometimes.

Written 2018 by Mikey Dickerson.
"""
import argparse
import os
import random
import re
import subprocess
import sys


"""
ffmpeg displays track language metadata in ISO 639-1 2-letter codes.
However it does not accept these as input--it silently changes any
two letter code to 'eng'.  It only accepts ISO 639-2 3-letter codes.
Yeah.

If you should need to look up some more:

https://en.wikipedia.org/wiki/List_of_ISO_639-2_codes
"""

lang_iso_code = {
  'ar': 'ara', # Arabic
  'cy': 'cym', # Welsh
  'de': 'ger', # German
  'en': 'eng', # English
  'es': 'spa', # Spanish
  'fa': 'fas', # Persian
  'fr': 'fra', # French
  'ga': 'gle', # Irish
  'he': 'heb', # Hebrew
  'ja': 'jpn', # Japanese
  'ko': 'kor', # Korean
  'pt': 'por', # Portuguese
  'ru': 'rus', # Russian
  'zh': 'chi', # Chinese
  'unknown': 'eng',
}


class Stream(object):
  """Stream here is a container for metadata describing one of the streams
  in a multiplexed dvd dump.  Not something you read or write from."""

  def __init__(self, t, fmt, lang):
    self.type = t
    self.format = fmt
    self.language = lang

  def __str__(self):
    return ' '.join(map(str, [self.type, self.format or '', self.language]))


def msg(s, stream=sys.stdout):
  """Write s to stdout with ANSI color control codes."""
  stream.write(''.join(['\033[92m', s, '\033[0m', '\n']))
  stream.flush()


def die(err):
  msg(err, stream=sys.stderr)
  sys.exit(1)


def read_toc(n):
  """Use lsdvd to read the table of contents, print each 'Title' line,
  and find the n longest titles."""
  title_len = []
  for line in subprocess.Popen('lsdvd', stdout=subprocess.PIPE).stdout:
    line = line.strip()
    print line
    m = re.match('Title: (\d+), Length: ([0-9:\.]+)', line)
    if m: title_len.append((m.group(2), int(m.group(1))))

  # Guess at which dvd titles you meant to rip.  We pick the N longest
  # ones on the disc, but we put them back in the order they appeared.
  title_len.sort(key=lambda x: x[0], reverse=True)
  title_len = title_len[:n]
  title_len.sort(key=lambda x: x[1])
  return [ x[1] for x in title_len ]


def mktemp():
  """Real mktemp type functions are a problem here because the temp file
  isn't likely to fit in /tmp.  It is a much better assumption that it
  fits in cwd.  So this will have to do."""
  path = ['dvdrip.' ]
  for i in range(6):
    path.append(random.choice('abcdefghijklmnopqrstuvwxyz'))
  path.append('.vob')
  return ''.join(path)


def parse_metadata(log):
  """Parse the stdout output from mplayer and look for the metadata
  identifying the format and language of the streams in the
  multiplexed dump."""
  streams = []
  audio_re = re.compile(r'audio stream: \d+ format: ([\w\.]+) \(([\w\.]+)\) '
                        'language: (\w+)')
  subtitle_re = re.compile(r'subtitle \( sid \): \d+ language: (\w+)')

  for line in log:
    line = line.strip()
    m = audio_re.match(line)
    if m:
      stream = Stream('audio', '%s:%s' % (m.group(1), m.group(2)),
                      lang_iso_code[m.group(3)])
      msg('Spotted stream: %s' % stream)
      streams.append(stream)
    m = subtitle_re.match(line)
    if m:
      stream = Stream('subtitle', None, lang_iso_code[m.group(1)])
      msg('Spotted stream: %s' % stream)
      streams.append(stream)

  return streams


def rip(t, dumpfile):
  """Use mplayer to dump a given title off the dvd.  Capture the stdout
  output and send to parse_metadata to find the stream descriptors."""
  msg('Ripping title %d' % t)
  cmd = [ 'mplayer', '-dumpstream', 'dvd://%d' % t,
          '-nocache', '-noidx', '-dumpfile', tmp ]
  msg('Running: %s' % ' '.join(cmd))
  log = subprocess.Popen(cmd, stdout=subprocess.PIPE).stdout
  return parse_metadata(log)


def stream_language_tags(stream_defs):
  """Given a list of stream descriptor objects, return a list of -metadata
  arguments for ffmpeg to tag all the streams."""
  args = []
  astream, sstream = 0, 0
  for s in stream_defs:
    if s.type == 'audio':
      args.append('-metadata:s:a:%d' % astream)
      args.append('language=%s' % s.language)
      astream += 1
    elif s.type == 'subtitle':
      args.append('-metadata:s:s:%d' % sstream)
      args.append('language=%s' % s.language)
      sstream += 1
    else:
      assert 'wut?' == 0
  return args


def filesafe(s):
  """Deletes or substitutes the characters that are likely to cause
  non-portable filenames: anything Unicode, and (?*:/\#!"'<>)."""
  if not s: return ''
  s = s.encode('ascii', 'replace')
  badchar = r'?*"\'!<>()'
  for c in badchar: s = s.replace(c, '')
  badchar = r':/\#'
  for c in badchar: s = s.replace(c, '-')
  return s


def re_mux(tmpfile, metadata_bag):
  """You have to re-multiplex the entire file to get metadata tags into
  it.  The bare minimum way to do this is to pass all the streams
  through the ffmpeg 'copy' codec.  Unfortunately this exposes us to
  all the (many) bugs and brittle-by-design behaviors of the decoder
  and multiplexer."""
  map_args = ['-map', '0']
  if 'episode_id' in metadata_bag:
    outfile = filesafe("%s %s.mp4" % (metadata_bag['episode_id'],
                                      metadata_bag['title']))
  else:
    outfile = filesafe("%s.mp4" % metadata_bag['title'])


  while True:
    cmd = [ 'ffmpeg',
            # These are necessary to have ffmpeg look far enough into the input
            # file to identify the streams.
            '-probesize', '200M', '-analyzeduration', '120M',
            # The thousands of 'pts has no value' errors are annoying.  You can
            # eliminate them with '-fflags +genpts'.  But this contains memory
            # leaks that crash ffmpeg if the input is large.  Too bad.
            ##'-fflags', '+genpts',
            # Input file.
            '-i',  tmpfile ]
    # Map arguments have to come immediately after input file.
    cmd.extend(map_args)
    # Apply 'copy' codec to all mapped streams.
    cmd.extend(['-c', 'copy'])
    # Overwrite output file without asking.  Necessary if we want to try again
    # after a recoverable error.
    cmd.append('-y')
    # Now you can do metadata arguments.
    for k, v in metadata_bag.iteritems():
      cmd.append('-metadata')
      cmd.append('%s=%s' % (k, str(v)))
    cmd.extend(stream_language_tags(streams))
    # Output file is the only positional argument, and has to go last.
    cmd.append(outfile)

    child = subprocess.Popen(cmd, stdout=subprocess.PIPE, stdin=subprocess.PIPE,
                             stderr=subprocess.PIPE)
    stdout, stderr = child.communicate(input=None)
    status = child.wait()

    if status == 0:
      msg('ffmpeg returned ok')
      msg('Deleting input file %s' % tmpfile)
      os.unlink(tmpfile)
      break

    if status < 0:
      msg('ffmpeg terminated by signal %d' % -status)
    elif status > 0:
      msg('ffmpeg returned error status %d' % status)

    # Look for one of the errors we understand.
    err_re = re.compile(r'non monotonically increasing dts')
    if err_re.search(stderr):
        msg('Subtitles are impossible to represent in mpeg.')
        msg('Trying again with no subtitles.')
        map_args = ['-map', '0:v', '-map', '0:a']
        continue

    err_re = re.compile(r'Could not find codec parameters for stream (\d+)')
    m = err_re.search(stderr)
    if m:
      bad_stream = m.group(1)
      msg('Cannot interpret stream %s.' % bad_stream)
      msg('Trying again with that stream excluded.')
      # now for some fun: constructing a set of -map args that excludes only
      # the bad stream
      map_args = []
      stream_re = re.compile(r'Stream #0:(\d+)')
      for line in stderr.split('\n'):
        # make sure to stop reading after the end of the 'input' section
        if line.startswith('Output #0'): break
        m = stream_re.search(line)
        if m and m.group(1) != bad_stream:
          map_args.extend(['-map', '0:%s' % m.group(1)])
      continue

    # If we made it here, we don't know why ffmpeg crashed.  Good luck!
    msg('Unrecoverable error in ffmpeg.')
    msg('Leaving input file %s behind.' % tmpfile)
    msg('Dumping stdout, stderr, and sh command to ffmpeg.*')
    open('ffmpeg.stdout', 'w').write(stdout)
    open('ffmpeg.stderr', 'w').write(stderr)
    open('ffmpeg.sh', 'w').write(' '.join(cmd))
    die('Good luck!')


def add_arguments(parser):

  parser.add_argument("-t", "--title", type=str, dest='title',
                      help="Movie title, or episode titles delimited with |")
  parser.add_argument("-y", "--year", type=str, dest='year',
                      help="Year")
  parser.add_argument("-s", "--show", type=str, dest='show',
                      help="Name of TV show")
  parser.add_argument("-e", "--episodes", type=str, dest='episodes', default='',
                      help="Episode identifiers delimited with |")


if __name__ == '__main__':
  global ARGS
  parser = argparse.ArgumentParser()
  add_arguments(parser)
  ARGS = parser.parse_args()

  if not ARGS.title:
    die('must supply title')

  titles = ARGS.title.split('|')
  episodes = ARGS.episodes.split('|')

  if len(titles) != len(episodes):
    die('got %d titles, but %d episode IDs' % (len(titles), len(episodes)))

  tracks = read_toc(len(titles))
  msg('\nDefault tracks: %s\n' % ' '.join(map(str, tracks)))
  msg('Enter to continue, type new track numbers, or Ctrl-C.')
  try:
    new_tracks = sys.stdin.readline()
  except KeyboardInterrupt:
    die('Bye Bye')
  new_tracks = new_tracks.strip()
  if new_tracks:
    tracks = map(int, new_tracks.split())

  if len(tracks) != len(titles):
    die('selected %d tracks to rip, but number of tags is %d'
        % (len(tracks), len(titles)))

  tmp = mktemp()
    
  for i, track in enumerate(tracks):
    title, episode = titles[i], episodes[i]
    streams = rip(track, tmp)
    metadata = {'title': title,
                'description': '/'.join(map(str, streams)) }
    if episode: metadata['episode_id'] = episode
    if ARGS.show: metadata['show'] = ARGS.show
    if ARGS.year: metadata['date'] = ARGS.year
    re_mux(tmp, metadata)

  msg('See You Space Cowboy')
