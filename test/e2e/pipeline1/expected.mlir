module {
  hw.module @main(%clk: i1, %rst: i1) {
    %c0 = hw.constant 5 : i32
    %c1 = hw.constant 0 : i32
    %c2 = hw.constant 5 : i32
    %c3 = hw.constant 426771240 : i32
    %c4 = hw.constant 0 : i32
    %c5 = hw.constant 537200675 : i32
    %c6 = hw.constant 537334308 : i32
    %c7 = hw.constant 1 : i32
    %c8 = hw.constant 426770689 : i32
    %c9 = hw.constant 5 : i32
    %c10 = hw.constant 0 : i32
    %c11 = hw.constant 5 : i32
    %c12 = hw.constant 1 : i32
    %c13 = hw.constant 426771240 : i32
    %c14 = hw.constant 426770689 : i32
    %c15 = hw.constant 1 : i32
    %c16 = hw.constant 2 : i32
    %c17 = hw.constant 5 : i32
    %c18 = hw.constant 1 : i32
    %c19 = hw.constant true : i1
    // channel t0 depth=1 type=i32
    mygo.channel.full "t0"(1/1)
    mygo.channel.valid "t0"(3) {process = "source"}
    mygo.channel.valid "t0"(3) {process = "source"}
    mygo.channel.ready "t0"(2) {process = "filter"}
    mygo.channel.ready "t0"(2) {process = "filter"}
    // channel t1 depth=4 type=i32
    mygo.channel.full "t1"(2/4)
    mygo.channel.valid "t1"(2) {process = "filter"}
    mygo.channel.valid "t1"(2) {process = "filter"}
    mygo.channel.ready "t1"(1) {process = "sink"}
    mygo.channel.ready "t1"(1) {process = "sink"}
    // channel t2 depth=1 type=i1
    mygo.channel.full "t2"(0/1)
    mygo.channel.valid "t2"(1) {process = "sink"}
    mygo.channel.ready "t2"(0) {process = "main"}
    mygo.process.spawn "sink"() channels ["t1", "t2"] {stage = 1, parent_stage = 0}
    mygo.process.spawn "filter"() channels ["t0", "t1"] {stage = 2, parent_stage = 0}
    mygo.process.spawn "source"() channels ["t0"] {stage = 3, parent_stage = 0}
    %v20 = mygo.channel.recv "t2" : i1
    %v21 = mygo.channel.recv "t1" : i32
    // phi t1_1 has 2 incoming values
    %v22 = comb.icmp ult, %t1_1, %c17 : i32
    mygo.channel.send "t2"(%c19) : i1
    %v23 = mygo.channel.recv "t1" : i32
    %v24 = comb.add %t1_1, %c18 : i32
    %v25 = mygo.channel.recv "t0" : i32
    mygo.channel.send "t1"(%c0) : i32
    // phi t1_11 has 2 incoming values
    %v26 = comb.icmp ult, %t1_11, %c2 : i32
    %v27 = mygo.channel.recv "t0" : i32
    %v28 = comb.icmp eq, %v27, %c3 : i32
    %v29 = comb.icmp eq, %v27, %c8 : i32
    // phi t5_19 has 3 incoming values
    mygo.channel.send "t1"(%t5_19) : i32
    %v30 = comb.add %t1_11, %c7 : i32
    mygo.channel.send "t0"(%c9) : i32
    // phi t0_26 has 2 incoming values
    %v31 = comb.icmp ult, %t0_26, %c11 : i32
    %v32 = comb.icmp eq, %t0_26, %c12 : i32
    %v33 = comb.icmp eq, %t0_26, %c16 : i32
    // phi t3_33 has 3 incoming values
    mygo.channel.send "t0"(%t3_33) : i32
    %v34 = comb.add %t0_26, %c15 : i32
    hw.output
  }
}
