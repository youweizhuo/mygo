module {
  hw.module @main(%clk: i1, %rst: i1) {
    %c0 = hw.constant 0 : i32
    %c1 = hw.constant 1 : i32
    %c2 = hw.constant 0 : i32
    %c3 = hw.constant 4 : i32
    %c4 = hw.constant true : i1
    %c5 = hw.constant true : i1
    %c6 = hw.constant 0 : i32
    %c7 = hw.constant 1 : i32
    %c8 = hw.constant 0 : i32
    %c9 = hw.constant 0 : i32
    %c10 = hw.constant 0 : i32
    %c11 = hw.constant 4 : i32
    %c12 = hw.constant 1 : i32
    %c13 = hw.constant 10 : i32
    %c14 = hw.constant 1 : i32
    %c15 = hw.constant 1 : i32
    %c16 = hw.constant 1 : i32
    %c17 = hw.constant 0 : i32
    %c18 = hw.constant 4 : i32
    %c19 = hw.constant 2 : i32
    %c20 = hw.constant 1 : i32
    %c21 = hw.constant true : i1
    %c22 = hw.constant 1 : i32
    %c23 = hw.constant true : i1
    // channel t0 depth=1 type=i32
    mygo.channel.full "t0"(1/1)
    mygo.channel.valid "t0"(5) {process = "producer"}
    mygo.channel.ready "t0"(3) {process = "router"}
    // channel t1 depth=1 type=i32
    mygo.channel.full "t1"(0/1)
    mygo.channel.ready "t1"(3) {process = "router"}
    // channel t2 depth=1 type=i32
    mygo.channel.full "t2"(0/1)
    mygo.channel.ready "t2"(2) {process = "consumer"}
    // channel t3 depth=1 type=i32
    mygo.channel.full "t3"(0/1)
    // channel t4 depth=1 type=i1
    mygo.channel.full "t4"(0/1)
    mygo.channel.valid "t4"(3) {process = "router"}
    mygo.channel.ready "t4"(5) {process = "producer"}
    // channel t5 depth=1 type=i1
    mygo.channel.full "t5"(1/1)
    mygo.channel.valid "t5"(3) {process = "router"}
    // channel t6 depth=1 type=i1
    mygo.channel.full "t6"(1/1)
    mygo.channel.valid "t6"(2) {process = "consumer"}
    // channel t7 depth=1 type=i1
    mygo.channel.full "t7"(0/1)
    // channel t8 depth=2 type=i1
    mygo.channel.full "t8"(0/2)
    mygo.channel.valid "t8"(2) {process = "consumer"}
    mygo.channel.ready "t8"(0) {process = "main"}
    mygo.process.spawn "consumer"(%c0) channels ["t2", "t6", "t8"] {stage = 2, parent_stage = 0}
    mygo.process.spawn "consumer"(%c1) channels ["t3", "t7", "t8"] {stage = 2, parent_stage = 0}
    mygo.process.spawn "router"() channels ["t0", "t1", "t4", "t5", "t2", "t3", "t6", "t7"] {stage = 3, parent_stage = 0}
    mygo.process.spawn "producer"(%c8, %c9) channels ["t0", "t4"] {stage = 5, parent_stage = 0}
    mygo.process.spawn "producer"(%c15, %c16) channels ["t1", "t5"] {stage = 5, parent_stage = 0}
    // phi t27_38 has 2 incoming values
    %v24 = comb.icmp slt, %t27_38, %c19 : i32
    %v25 = mygo.channel.recv "t8" : i1
    %v26 = comb.add %t27_38, %c20 : i32
    // phi t0_1 has 2 incoming values
    %v27 = comb.icmp slt, %t0_1, %c18 : i32
    mygo.channel.send "t8"(%c23) : i1
    %v28 = mygo.channel.recv "t2" : i32
    %v29 = seq.compreg %v28, %clk : i32
    mygo.channel.send "t6"(%c21) : i1
    %v30 = comb.add %t0_1, %c22 : i32
    // phi t0_11 has 2 incoming values
    %v31 = comb.icmp slt, %t0_11, %c3 : i32
    %v32 = mygo.channel.recv "t0" : i32
    mygo.channel.send "t4"(%c4) : i1
    %v33 = mygo.channel.recv "t1" : i32
    mygo.channel.send "t5"(%c5) : i1
    %v34 = comb.add %t0_11, %c7 : i32
    // phi t0_23 has 2 incoming values
    %v35 = comb.icmp slt, %t0_23, %c11 : i32
    %v36 = comb.add %c9, %t0_23 : i32
    %v37 = comb.and %v36, %c12 : i32
    %v38 = comb.mul %c8, %c13 : i32
    %v39 = comb.add %v38, %t0_23 : i32
    mygo.channel.send "t0"(%complit) : i32
    %v40 = mygo.channel.recv "t4" : i1
    %v41 = comb.add %t0_23, %c14 : i32
    hw.output
  }
}
