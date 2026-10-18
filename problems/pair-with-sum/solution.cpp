#include <bits/stdc++.h>
using namespace std;

int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);

    int n;
    long long s;
    cin >> n >> s;
    unordered_map<long long, int> seen;
    seen.reserve(n * 2);
    for (int j = 1; j <= n; j++) {
        long long x;
        cin >> x;
        auto it = seen.find(s - x);
        if (it != seen.end()) {
            cout << it->second << ' ' << j << '\n';
            return 0;
        }
        seen.emplace(x, j);
    }
    cout << -1 << '\n';
    return 0;
}
