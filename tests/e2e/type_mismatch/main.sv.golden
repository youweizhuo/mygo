module main(
  input clk,
        rst
);

  wire [15:0] _GEN = {8'h0, 8'h5} - 16'hC;
  always @(posedge clk)
    $fwrite(32'h80000001, "acc=%d wide=%d\n", _GEN, {{16{_GEN[15]}}, _GEN} + 32'h400);
endmodule

