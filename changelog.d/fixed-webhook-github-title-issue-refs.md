- GitHub pull-request comments posted by `atcr webhook` now break every `#<number>`
  issue reference in an untrusted finding title, including the cross-repo
  `owner/repo#7` shape and a `#` right after a word, so a model-authored title can
  no longer backlink another issue. The trusted "ATCR review of owner/repo#N"
  heading keeps its PR link, and Slack replies are unchanged. (epic 132 T1)
