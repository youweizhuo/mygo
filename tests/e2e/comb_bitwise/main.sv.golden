module main(
  input clk,
        rst
);

  always @(posedge clk)
    $fwrite(32'h80000001, "and=0x%x or=0x%x xor=0x%x\n", 16'h33CC & 16'hF0F,
            16'h33CC | 16'hF0F, 16'h33CC ^ 16'hF0F);
endmodule

