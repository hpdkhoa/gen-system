      * EMPREC: one employee record, as the HR extract writes it.
       01 EMPLOYEE-RECORD.
          05 EMP-ID            PIC 9(6).
          05 EMP-NAME          PIC X(30).
          05 EMP-GRADE         PIC X(1).
          05 EMP-HOURS         PIC 9(3)V9.
          05 EMP-RATE          PIC 9(4)V99.
          05 EMP-REGION        PIC X(2).
