import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.StreamTokenizer;

// Spins for 1200 ms before answering. The limit is 2 s for Java (factor 2) and
// the JVM's own start-up adds CPU time, so the total is meant to land between
// 50 and 90 percent of it. The spin is by wall clock, so on a loaded machine
// it uses less CPU, never more.
public class Main {
    public static void main(String[] args) throws IOException {
        long end = System.nanoTime() + 1200L * 1_000_000L;
        while (System.nanoTime() < end) {}
        StreamTokenizer in = new StreamTokenizer(new BufferedReader(new InputStreamReader(System.in)));
        in.nextToken();
        int n = (int) in.nval;
        long distance = 0, hours = 0;
        for (int i = 0; i < n; i++) {
            in.nextToken();
            distance += (long) in.nval;
            in.nextToken();
            hours += (long) in.nval;
        }
        System.out.printf("%.9f%n", (double) distance / hours);
    }
}
