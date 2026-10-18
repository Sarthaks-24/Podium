Given $n$ integers $a_1, \ldots, a_n$ and a target $s$, find two positions $i < j$ with $a_i + a_j = s$.

## Input

The first line holds $n$ and $s$. The second line holds $a_1, \ldots, a_n$.

## Output

Print two positions $i$ and $j$ with $1 \le i < j \le n$ and $a_i + a_j = s$. If there are several such pairs, print any of them. If there is none, print `-1`.

## Constraints

- $|a_i|, |s| \le 10^9$
- Subtask 1 (30 points): $2 \le n \le 2000$
- Subtask 2 (70 points): $2 \le n \le 2 \cdot 10^5$
