# Hostile: prints forever. CPython ignores SIGXFSZ, so only the output size
# reveals that it hit the cap.
line = "podium " * 8
while True:
    print(line)
