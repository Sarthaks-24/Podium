import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.StreamTokenizer;

public class Main {
    public static void main(String[] args) throws IOException {
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
