#!/bin/bash

set -e

: ${EDITOR:=vi}
: ${OUTFILE:=cd.flac}

msg ()
{
  echo -e "\033[92m$1\033[0m"
}

# Check for binaries.  type will return >0 if a program doesn't
# exist, then bash will die because of -e, so the 'not found'
# message will be your clue that you need to install it.

msg 'Checking for necessary binaries'
for B in cdparanoia flac metaflac ; do
  type -a $B
done

msg 'Ripping audio stream'
cdparanoia "1-"

msg 'Compressing audio stream'
flac -o "$OUTFILE" --delete-input-file -V --padding 262144 cdda.wav

msg 'Creating cuesheet'
TRACKS=$(cdparanoia -Q 2>&1 | awk '/^ *[0-9]+\. / { print $5; }' | tr -d '[]' | tr '.' ':')
I=1
(
  echo 'FILE "cdda.wav" WAVE'
  for T in $TRACKS ; do
    printf '  TRACK %02d AUDIO\n' $I
    printf '    INDEX 01 %s\n' $T
    I=$(($I + 1))
  done
) | metaflac --import-cuesheet-from=- "$OUTFILE"

msg 'Creating tags'
I=1
(
  echo 'ARTIST='
  echo 'ALBUM='
  echo 'DATE='
  for T in $TRACKS ; do
    printf 'TITLE[%d]=\n' $I
    I=$(($I + 1))
  done
) > tags

$EDITOR tags
metaflac --import-tags-from=tags "$OUTFILE"

ARTIST=$(grep 'ARTIST=' tags | cut -d= -f2)
ALBUM=$(grep 'ALBUM=' tags | cut -d= -f2)
if [ -n "$ARTIST" -a -n "$ALBUM" ] ; then
  NEWFILE="$ARTIST - $ALBUM.flac"
  mv -v "$OUTFILE" "$NEWFILE"
  msg "Finished file is \'$NEWFILE\'"
fi

msg "Remember to store the cover image:"
msg "metaflac --import-picture-from=xyz.jpg \'$NEWFILE\'"

