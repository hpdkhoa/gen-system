       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
      * A small batch payroll run, written for the gen-system examples.
      * It reads employees, computes gross, tax and net pay, writes a
      * payslip line per employee and records each run in the database.
       ENVIRONMENT DIVISION.
       INPUT-OUTPUT SECTION.
       FILE-CONTROL.
           SELECT EMPLOYEE-FILE ASSIGN TO "EMPLOYEES.DAT"
               ORGANIZATION IS LINE SEQUENTIAL.
           SELECT PAYSLIP-FILE ASSIGN TO "PAYSLIPS.DAT"
               ORGANIZATION IS LINE SEQUENTIAL.
       DATA DIVISION.
       FILE SECTION.
       FD EMPLOYEE-FILE.
       COPY EMPREC.
       FD PAYSLIP-FILE.
       COPY PAYSLIP.
       WORKING-STORAGE SECTION.
       01 WS-EOF              PIC X VALUE "N".
       01 WS-GROSS            PIC 9(7)V99.
       01 WS-TAX              PIC 9(7)V99.
       01 WS-TAX-RATE         PIC V999.
       01 WS-OVERTIME         PIC 9(3)V9.
       01 WS-COUNT            PIC 9(6) VALUE 0.
       PROCEDURE DIVISION.
       MAIN-PARA.
           OPEN INPUT EMPLOYEE-FILE
           OPEN OUTPUT PAYSLIP-FILE
           PERFORM UNTIL WS-EOF = "Y"
               READ EMPLOYEE-FILE
                   AT END
                       MOVE "Y" TO WS-EOF
                   NOT AT END
                       PERFORM PROCESS-EMPLOYEE
               END-READ
           END-PERFORM
           PERFORM RECORD-RUN
           CLOSE EMPLOYEE-FILE
           CLOSE PAYSLIP-FILE
           STOP RUN.

       PROCESS-EMPLOYEE.
           PERFORM CALC-GROSS
           PERFORM CALC-TAX
           MOVE EMP-ID TO PS-EMP-ID
           MOVE WS-GROSS TO PS-GROSS
           MOVE WS-TAX TO PS-TAX
           COMPUTE PS-NET = WS-GROSS - WS-TAX
           WRITE PAYSLIP-RECORD
           ADD 1 TO WS-COUNT.

       CALC-GROSS.
           IF EMP-HOURS > 40
               COMPUTE WS-OVERTIME = EMP-HOURS - 40
               COMPUTE WS-GROSS = 40 * EMP-RATE
                   + WS-OVERTIME * EMP-RATE * 1.5
           ELSE
               COMPUTE WS-GROSS = EMP-HOURS * EMP-RATE
           END-IF.

       CALC-TAX.
           EVALUATE EMP-GRADE
               WHEN "A"
                   MOVE .350 TO WS-TAX-RATE
               WHEN "B"
                   MOVE .250 TO WS-TAX-RATE
               WHEN "C"
                   MOVE .150 TO WS-TAX-RATE
               WHEN OTHER
                   MOVE .100 TO WS-TAX-RATE
           END-EVALUATE
           IF EMP-REGION = "NY"
               ADD .040 TO WS-TAX-RATE
           END-IF
           COMPUTE WS-TAX = WS-GROSS * WS-TAX-RATE.

       RECORD-RUN.
           EXEC SQL
               INSERT INTO PAYROLL_RUNS (RUN_DATE, EMPLOYEES)
               VALUES (CURRENT DATE, :WS-COUNT)
           END-EXEC
           CALL "AUDITLOG" USING WS-COUNT.
