# Y11S1 fork replay corpus

A local, **uncommitted** set of 46 real Y11S1 rounds (builds 9540659 and 9578674), used by the match-level acceptance test `TestAcceptance_Matches`.

The rounds come from two forks that committed them: nicktstewart/r6-dissect (`0b5f223`, build 9540659) and ArnaudCabello/r6-dissect (`dfdc96d`, build 9578674). They are third-party players' data with no data licence, and they contain usernames and profile IDs, so they are never committed here. `y11s1-forks.tsv` pins each file by git blob SHA (columns: blob SHA, build, source ref, source path, target path), with no player names. Pinning by SHA makes a force-push in a fork harmless as long as someone still holds the objects.

## Rebuild it

```bash
scripts/corpus/fetch.sh ~/r6-corpus/y11s1
R6_DISSECT_CORPUS=~/r6-corpus/y11s1 go test ./dissect/test -run TestAcceptance_Matches -v
```

`fetch.sh` fetches the fork branches named in the manifest into a scratch bare repo next to the output directory (`<out-dir>.objects.git`), reads each pinned blob with `git cat-file`, and checks it with `git hash-object`. It never checks out, builds or runs fork code, and it refuses an output directory inside this repository. To fetch from a local mirror that has `refs/remotes/<owner>/<branch>` for each fork instead of GitHub, set `R6_CORPUS_MIRROR=<git-dir>`.

## Known failures

`TestAcceptance_Matches` stays green on this corpus while exactly the failures listed in `knownMatchFailures` (`dissect/test/acceptance_match_test.go`) occur, each naming the issue or run-log candidate that owns it. When a fix lands, the test goes red for that entry: delete the entry. A new failing folder or invariant is red too.

## Other corpora

Any directory whose subdirectories each hold one match's `.rec` files works as `R6_DISSECT_CORPUS`, for example your own matches. `R6_DISSECT_REPLAY_DIRS` (round level) is a separate variable that belongs to `TestAcceptance_Replays`.
