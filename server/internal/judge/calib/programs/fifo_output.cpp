// Hostile: replaces its output file with a FIFO. A judge that opened it
// with a blocking read would wait forever and lose the lane.
#include <sys/stat.h>
#include <unistd.h>
int main() {
    unlink("output.txt");
    return mkfifo("output.txt", 0666) == 0 ? 0 : 1;
}
