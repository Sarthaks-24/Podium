A runner covers $n$ segments of a route. Segment $i$ is $d_i$ kilometres long and takes $h_i$ hours. Find the runner's average speed over the whole route, in kilometres per hour.

## Input

The first line holds $n$. Each of the next $n$ lines holds two integers $d_i$ and $h_i$.

## Output

Print the average speed. Answers within an absolute or relative error of $10^{-6}$ are accepted.

## Constraints

- $1 \le d_i, h_i \le 1000$
- Subtask 1 (30 points): $n = 1$
- Subtask 2 (70 points): $1 \le n \le 10^5$

Average speed is total distance divided by total time, not the mean of the segment speeds.
