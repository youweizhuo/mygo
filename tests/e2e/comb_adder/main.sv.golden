module main(
  input clk,
        rst
);

  wire [15:0] _GEN = 16'hFFF4;
  wire [15:0] _GEN_0 = 16'h19;
  wire [31:0] _GEN_1 = {{16{_GEN[15]}}, _GEN} + {{16{_GEN_0[15]}}, _GEN_0};
  always @(posedge clk)
    $fwrite(32'h80000001, "partial=%d widened=0x%x\n", _GEN_1,
            {16'h0, _GEN_1[15:0]} + {16'h0, 16'h1234});
endmodule

