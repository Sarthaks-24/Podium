Write a function that returns the sum of the numbers in an array.

You write only the function. The code above and below it is locked: it reads the input, calls your function and prints what it returns.

| Language | Your function |
|---|---|
| C++17 | `long long arraySum(const vector<int>& a)` |
| Java 21 | `long arraySum(int[] a)` inside `class Solution` |
| Python 3 | `def array_sum(a)` |
| JavaScript | `function arraySum(a)` |

## Input

The first line holds $n$, the length of the array. The second line holds the $n$ integers $a_1, \ldots, a_n$.

## Output

Print the sum of the array.

## Constraints

- $1 \le n \le 20\,000$
- $-10^9 \le a_i \le 10^9$
- Subtask 1: $|a_i| \le 1000$, so the sum fits in a 32-bit integer.
- Subtask 2: no other limits; the sum needs a 64-bit integer.
