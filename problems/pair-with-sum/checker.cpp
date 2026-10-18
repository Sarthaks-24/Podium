// Accepts any valid pair. The expected answer only says whether a pair
// exists: "-1" when none does, any valid pair otherwise.
#include "testlib.h"
#include <vector>

int main(int argc, char* argv[]) {
    registerTestlibCmd(argc, argv);

    int n = inf.readInt();
    long long s = inf.readLong();
    std::vector<long long> a(n);
    for (auto& x : a) x = inf.readLong();

    bool exists = ans.readInt() != -1;
    int i = ouf.readInt(-1, n, "i");
    if (i == -1) {
        if (exists) quitf(_wa, "a pair exists, but the output says -1");
        quitf(_ok, "no pair exists");
    }
    int j = ouf.readInt(1, n, "j");
    if (i < 1 || i >= j) quitf(_wa, "positions must satisfy 1 <= i < j, got %d %d", i, j);
    if (a[i - 1] + a[j - 1] != s) quitf(_wa, "a[%d] + a[%d] is not %lld", i, j, s);
    if (!exists) quitf(_fail, "the output found a pair the expected answer missed");
    quitf(_ok, "valid pair %d %d", i, j);
}
