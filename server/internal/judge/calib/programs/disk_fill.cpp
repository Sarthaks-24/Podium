// Hostile: writes 16 MB files into /tmp until something stops it (at most
// 2 GB). Each file is under the --fsize cap, so only the total is limited:
// with the box root on tmpfs, the pages count against the box's memory
// limit. Exits 0 only if all 2 GB were written, which means disk use is
// unbounded.
#include <cstdio>
#include <vector>
int main() {
    std::vector<char> chunk(16 << 20, 'x');
    for (int i = 0; i < 128; i++) {
        char name[32];
        snprintf(name, sizeof name, "/tmp/fill%03d", i);
        FILE *f = fopen(name, "wb");
        if (!f) return 1;
        size_t n = fwrite(chunk.data(), 1, chunk.size() - 1, f);
        if (fclose(f) != 0 || n != chunk.size() - 1) return 1;
    }
    return 0;
}
