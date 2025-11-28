module {
  hw.module @main(%clk: i1, %rst: i1) {
    %c0 = hw.constant 0 : i32
    %c1 = hw.constant 4 : i32
    %c2 = hw.constant 1 : i32
    %c3 = hw.constant 4 : i32
    %c4 = hw.constant 0 : i32
    %c5 = hw.constant 4 : i32
    %c6 = hw.constant 1 : i32
    %c7 = hw.constant 4 : i32
    %c8 = hw.constant 1 : i32
    %c9 = hw.constant true : i1
    %c10 = hw.constant 0 : i32
    // channel t0 depth=1 type=i32
    mygo.channel.full "t0"(1/1)
    mygo.channel.valid "t0"(3) {process = "stage1"}
    mygo.channel.valid "t0"(3) {process = "stage1"}
    mygo.channel.ready "t0"(2) {process = "stage2"}
    mygo.channel.ready "t0"(2) {process = "stage2"}
    // channel t1 depth=8 type=i8
    mygo.channel.full "t1"(0/8)
    // channel t2 depth=1 type=i1
    mygo.channel.full "t2"(0/1)
    mygo.channel.valid "t2"(1) {process = "stage3"}
    mygo.channel.ready "t2"(0) {process = "main"}
    mygo.process.spawn "stage3"() channels ["t1", "t2"] {stage = 1, parent_stage = 0}
    mygo.process.spawn "stage2"() channels ["t0", "t1"] {stage = 2, parent_stage = 0}
    mygo.process.spawn "stage1"() channels ["t0"] {stage = 3, parent_stage = 0}
    %v11 = mygo.channel.recv "t2" : i1
    // phi t1_0 has 2 incoming values
    %v12 = comb.icmp ult, %t1_0, %c7 : i32
    mygo.channel.send "t2"(%c9) : i1
    %v13 = comb.add %t1_0, %c8 : i32
    %v14 = mygo.channel.recv "t0" : i32
    // phi t2_8 has 2 incoming values
    %v15 = comb.icmp ult, %t2_8, %c1 : i32
    %v16 = mygo.channel.recv "t0" : i32
    %v17 = comb.add %t2_8, %c2 : i32
    mygo.channel.send "t0"(%c3) : i32
    // phi t0_16 has 2 incoming values
    %v18 = comb.icmp ult, %t0_16, %c5 : i32
    %v19 = comb.add %t0_16, %t0_16 : i32
    mygo.channel.send "t0"(%v19) : i32
    %v20 = comb.add %t0_16, %c6 : i32
    hw.output
  }
}
