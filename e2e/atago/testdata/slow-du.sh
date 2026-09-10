#!/bin/sh
# A command that keeps printing, in the shape of du output. Detection
# reads the leading lines before it commits, so the first burst is wider
# than the two hundred lines du declares, which is the widest window a
# definition may ask for. After the burst a line is printed every tenth
# of a second, the way a monitoring command prints a sample per
# interval: the burst is what gets a reader its records, and the paced
# lines are what jz's next write fails on once the reader has gone, and
# what is left unprinted when jz stops the command. The process id is
# what a scenario checks afterwards.
echo $$ > producer.pid
i=1
while [ $i -le 205 ]; do
  printf '%s\t/mnt/%s\n' "$((i * 4))" "$i"
  i=$((i + 1))
done
while [ $i -le 1000 ]; do
  sleep 0.1
  printf '%s\t/mnt/%s\n' "$((i * 4))" "$i"
  i=$((i + 1))
done
