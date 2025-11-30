module main(
  input clk,
        rst
);

  wire [31:0] _GEN = 32'h8;
  wire [31:0] _GEN_0 = 32'hC;
  wire [15:0] _GEN_1 = {8'h0, 8'hAB} << _GEN[15:0] | {8'h0, 8'hCD};
  always @(posedge clk)
    $fwrite(32'h80000001, "word=0x%x high=%d low=0x%x\n", _GEN_1,
            _GEN_1 >> _GEN_0[15:0] & 16'hF, _GEN_1 & 16'hFF);
endmodule

