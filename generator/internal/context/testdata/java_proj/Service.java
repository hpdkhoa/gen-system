package com.example;
import java.util.ArrayList;

public class Service {
    private ArrayList items;

    public void run() {
        items = new ArrayList();
        items.add("hello");
        int n = size();
    }

    public int size() {
        return items.size();
    }
}
